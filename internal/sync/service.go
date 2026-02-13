package sync

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/model"
	"github.com/eslider/mails/internal/search/index"
	sync_gmail "github.com/eslider/mails/internal/sync/gmail"
	sync_imap "github.com/eslider/mails/internal/sync/imap"
	sync_pop3 "github.com/eslider/mails/internal/sync/pop3"
)

// syncEntry tracks a running sync's cancel function and progress.
type syncEntry struct {
	cancel    context.CancelFunc
	startedAt time.Time
	progress  string // human-readable status
	lastError string
}

// Service orchestrates email sync for all accounts of a user.
type Service struct {
	mu       sync.Mutex
	usersDir string
	accounts *account.Store
	running  map[string]*syncEntry // accountID -> entry
}

// NewService creates a sync service.
func NewService(usersDir string, accounts *account.Store) *Service {
	return &Service{
		usersDir: usersDir,
		accounts: accounts,
		running:  make(map[string]*syncEntry),
	}
}

// SyncAccount triggers a sync for a single account. Non-blocking; runs in background.
func (s *Service) SyncAccount(userID, accountID string) error {
	s.mu.Lock()
	if e, ok := s.running[accountID]; ok && e != nil {
		s.mu.Unlock()
		return fmt.Errorf("sync already running for account %s", accountID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.running[accountID] = &syncEntry{
		cancel:    cancel,
		startedAt: time.Now(),
		progress:  "starting",
	}
	s.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, accountID)
			s.mu.Unlock()
		}()

		acct, err := s.accounts.Get(userID, accountID)
		if err != nil {
			s.setProgress(accountID, "", "account not found: "+err.Error())
			log.Printf("ERROR: sync account %s: %v", accountID, err)
			return
		}

		stateDB, err := OpenStateDB(s.usersDir, userID)
		if err != nil {
			s.setProgress(accountID, "", "state db error: "+err.Error())
			log.Printf("ERROR: open sync state: %v", err)
			return
		}
		defer stateDB.Close()

		job, err := stateDB.CreateJob(accountID)
		if err != nil {
			s.setProgress(accountID, "", "create job error: "+err.Error())
			log.Printf("ERROR: create sync job: %v", err)
			return
		}
		job.Status = model.SyncStatusRunning
		stateDB.UpdateJob(job)

		emailDir := account.EmailDir(s.usersDir, userID, *acct)
		indexPath := account.IndexPath(s.usersDir, userID, *acct)

		// Start live indexing goroutine: rebuild index every 5s during sync.
		indexCtx, indexCancel := context.WithCancel(ctx)
		go s.liveIndex(indexCtx, emailDir, indexPath, accountID)

		s.setProgress(accountID, "syncing", "")
		newMsgs, syncErr := s.doSync(ctx, *acct, emailDir, stateDB, accountID)

		// Stop live indexing.
		indexCancel()

		now := time.Now()
		job.FinishedAt = &now
		job.NewMessages = newMsgs
		if syncErr != nil {
			job.Status = model.SyncStatusFailed
			job.Error = syncErr.Error()
			s.setProgress(accountID, "", syncErr.Error())
			log.Printf("ERROR: sync %s (%s): %v", acct.Email, acct.Type, syncErr)
		} else {
			job.Status = model.SyncStatusDone
			s.setProgress(accountID, "done", "")
			log.Printf("INFO: synced %s (%s): %d new messages", acct.Email, acct.Type, newMsgs)
		}
		stateDB.UpdateJob(job)

		// Final index rebuild after sync completes.
		s.rebuildIndex(emailDir, indexPath)
	}()

	return nil
}

// StopSync cancels a running sync for the given account.
func (s *Service) StopSync(accountID string) error {
	s.mu.Lock()
	entry, ok := s.running[accountID]
	s.mu.Unlock()

	if !ok || entry == nil {
		return fmt.Errorf("no sync running for account %s", accountID)
	}

	entry.cancel()
	log.Printf("INFO: sync cancelled for account %s", accountID)
	return nil
}

// SyncAll triggers sync for all accounts of a user.
func (s *Service) SyncAll(userID string) error {
	accounts, err := s.accounts.List(userID)
	if err != nil {
		return err
	}

	for _, acct := range accounts {
		if !acct.Sync.Enabled {
			continue
		}
		if err := s.SyncAccount(userID, acct.ID); err != nil {
			log.Printf("WARN: skip sync %s: %v", acct.Email, err)
		}
	}
	return nil
}

// IsRunning checks if a sync is currently running for an account.
func (s *Service) IsRunning(accountID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[accountID]
	return ok
}

// AccountStatus returns the current sync status for a single account.
func (s *Service) AccountStatus(userID string, acct model.EmailAccount) map[string]any {
	syncing := s.IsRunning(acct.ID)
	status := map[string]any{
		"id":      acct.ID,
		"name":    acct.Email,
		"type":    string(acct.Type),
		"syncing": syncing,
	}

	s.mu.Lock()
	if entry, ok := s.running[acct.ID]; ok {
		status["progress"] = entry.progress
		status["started_at"] = entry.startedAt.Unix()
		if entry.lastError != "" {
			status["last_error"] = entry.lastError
		}
	}
	s.mu.Unlock()

	stateDB, err := OpenStateDB(s.usersDir, userID)
	if err == nil {
		defer stateDB.Close()
		if job, err := stateDB.LastJob(acct.ID); err == nil && job != nil {
			if job.FinishedAt != nil {
				status["last_sync"] = job.FinishedAt.Unix()
			}
			status["new_messages"] = job.NewMessages
			if job.Error != "" && !syncing {
				status["last_error"] = job.Error
			}
		}
	}

	return status
}

func (s *Service) setProgress(accountID, progress, lastError string) {
	s.mu.Lock()
	if entry, ok := s.running[accountID]; ok {
		if progress != "" {
			entry.progress = progress
		}
		if lastError != "" {
			entry.lastError = lastError
		}
	}
	s.mu.Unlock()
}

func (s *Service) doSync(ctx context.Context, acct model.EmailAccount, emailDir string, stateDB *StateDB, accountID string) (int, error) {
	// Progress callback: update in-memory progress visible via API.
	onProgress := func(msg string) {
		s.setProgress(accountID, msg, "")
	}

	switch acct.Type {
	case model.AccountTypeIMAP:
		return sync_imap.SyncWithContext(ctx, acct, emailDir, stateDB, onProgress)
	case model.AccountTypePOP3:
		return sync_pop3.Sync(acct, emailDir, stateDB)
	case model.AccountTypeGmailAPI:
		return sync_gmail.Sync(acct, emailDir, stateDB)
	default:
		return 0, fmt.Errorf("unsupported account type: %s", acct.Type)
	}
}

// liveIndex rebuilds the search index every 5 seconds while sync is running.
func (s *Service) liveIndex(ctx context.Context, emailDir, indexPath, accountID string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.rebuildIndex(emailDir, indexPath)
			s.setProgress(accountID, "syncing (index updated)", "")
		}
	}
}

func (s *Service) rebuildIndex(emailDir, indexPath string) {
	idx, err := index.New(emailDir, indexPath)
	if err != nil {
		log.Printf("WARN: live index open: %v", err)
		return
	}
	defer idx.Close()
	idx.ClearCache()
	idx.Build()
	log.Printf("INFO: index rebuilt (%d emails)", idx.Stats().TotalEmails)
}
