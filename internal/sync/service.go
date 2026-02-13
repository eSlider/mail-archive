package sync

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/model"
	sync_gmail "github.com/eslider/mails/internal/sync/gmail"
	sync_imap "github.com/eslider/mails/internal/sync/imap"
	sync_pop3 "github.com/eslider/mails/internal/sync/pop3"
)

// Service orchestrates email sync for all accounts of a user.
type Service struct {
	mu        sync.Mutex
	usersDir  string
	accounts  *account.Store
	running   map[string]bool // accountID -> syncing
}

// NewService creates a sync service.
func NewService(usersDir string, accounts *account.Store) *Service {
	return &Service{
		usersDir: usersDir,
		accounts: accounts,
		running:  make(map[string]bool),
	}
}

// SyncAccount triggers a sync for a single account. Non-blocking; runs in background.
func (s *Service) SyncAccount(userID, accountID string) error {
	s.mu.Lock()
	if s.running[accountID] {
		s.mu.Unlock()
		return fmt.Errorf("sync already running for account %s", accountID)
	}
	s.running[accountID] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, accountID)
			s.mu.Unlock()
		}()

		acct, err := s.accounts.Get(userID, accountID)
		if err != nil {
			log.Printf("ERROR: sync account %s: %v", accountID, err)
			return
		}

		stateDB, err := OpenStateDB(s.usersDir, userID)
		if err != nil {
			log.Printf("ERROR: open sync state: %v", err)
			return
		}
		defer stateDB.Close()

		job, err := stateDB.CreateJob(accountID)
		if err != nil {
			log.Printf("ERROR: create sync job: %v", err)
			return
		}
		job.Status = model.SyncStatusRunning
		stateDB.UpdateJob(job)

		emailDir := account.EmailDir(s.usersDir, userID, *acct)
		newMsgs, syncErr := s.doSync(*acct, emailDir, stateDB)

		now := time.Now()
		job.FinishedAt = &now
		job.NewMessages = newMsgs
		if syncErr != nil {
			job.Status = model.SyncStatusFailed
			job.Error = syncErr.Error()
			log.Printf("ERROR: sync %s (%s): %v", acct.Email, acct.Type, syncErr)
		} else {
			job.Status = model.SyncStatusDone
			log.Printf("INFO: synced %s (%s): %d new messages", acct.Email, acct.Type, newMsgs)
		}
		stateDB.UpdateJob(job)
	}()

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
	return s.running[accountID]
}

// AccountStatus returns the current sync status for a single account.
func (s *Service) AccountStatus(userID string, acct model.EmailAccount) map[string]any {
	status := map[string]any{
		"id":      acct.ID,
		"name":    acct.Email,
		"type":    string(acct.Type),
		"syncing": s.IsRunning(acct.ID),
	}

	stateDB, err := OpenStateDB(s.usersDir, userID)
	if err == nil {
		defer stateDB.Close()
		if job, err := stateDB.LastJob(acct.ID); err == nil && job != nil {
			if job.FinishedAt != nil {
				status["last_sync"] = job.FinishedAt.Unix()
			}
			status["new_messages"] = job.NewMessages
			if job.Error != "" {
				status["last_error"] = job.Error
			}
		}
	}

	return status
}

func (s *Service) doSync(acct model.EmailAccount, emailDir string, stateDB *StateDB) (int, error) {
	switch acct.Type {
	case model.AccountTypeIMAP:
		return sync_imap.Sync(acct, emailDir, stateDB)
	case model.AccountTypePOP3:
		return sync_pop3.Sync(acct, emailDir, stateDB)
	case model.AccountTypeGmailAPI:
		return sync_gmail.Sync(acct, emailDir, stateDB)
	default:
		return 0, fmt.Errorf("unsupported account type: %s", acct.Type)
	}
}
