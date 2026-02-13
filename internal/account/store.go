// Package account manages per-user email account configurations.
package account

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eslider/mails/internal/model"
	"gopkg.in/yaml.v3"
)

const accountsFileName = "accounts.yml"

// Store manages email account configurations per user.
type Store struct {
	mu       sync.RWMutex
	usersDir string
}

// NewStore creates an account store.
func NewStore(usersDir string) *Store {
	return &Store{usersDir: usersDir}
}

// List returns all email accounts for a user.
func (s *Store) List(userID string) ([]model.EmailAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.load(userID)
}

// Get returns a single account by ID.
func (s *Store) Get(userID, accountID string) (*model.EmailAccount, error) {
	accounts, err := s.List(userID)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.ID == accountID {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("account %s not found", accountID)
}

// Create adds a new email account for a user.
func (s *Store) Create(userID string, acct model.EmailAccount) (*model.EmailAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, _ := s.load(userID)

	if acct.ID == "" {
		acct.ID = model.NewID()
	}
	if acct.Sync.Interval == "" {
		acct.Sync.Interval = "5m"
	}
	acct.Sync.Enabled = true

	accounts = append(accounts, acct)
	if err := s.save(userID, accounts); err != nil {
		return nil, err
	}

	// Create the email storage directory.
	emailDir := EmailDir(s.usersDir, userID, acct)
	os.MkdirAll(emailDir, 0o755)

	return &acct, nil
}

// Update replaces an existing account configuration.
func (s *Store) Update(userID string, acct model.EmailAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load(userID)
	if err != nil {
		return err
	}

	found := false
	for i, a := range accounts {
		if a.ID == acct.ID {
			accounts[i] = acct
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("account %s not found", acct.ID)
	}

	return s.save(userID, accounts)
}

// Delete removes an email account (does NOT delete downloaded emails).
func (s *Store) Delete(userID, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.load(userID)
	if err != nil {
		return err
	}

	filtered := make([]model.EmailAccount, 0, len(accounts))
	for _, a := range accounts {
		if a.ID != accountID {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == len(accounts) {
		return fmt.Errorf("account %s not found", accountID)
	}

	return s.save(userID, filtered)
}

// EmailDir returns the storage directory for an account's emails.
// Format: users/{userID}/{service-domain}/{local-part}/
// Example: users/019c.../gmail.com/eslider/
func EmailDir(usersDir, userID string, acct model.EmailAccount) string {
	domain, local := splitEmail(acct.Email)
	return filepath.Join(usersDir, userID, domain, local)
}

// IndexPath returns the parquet index path for an account.
// Format: users/{userID}/{service-domain}/{local-part}/index.parquet
func IndexPath(usersDir, userID string, acct model.EmailAccount) string {
	return filepath.Join(EmailDir(usersDir, userID, acct), "index.parquet")
}

func (s *Store) accountsPath(userID string) string {
	return filepath.Join(s.usersDir, userID, accountsFileName)
}

func (s *Store) load(userID string) ([]model.EmailAccount, error) {
	path := s.accountsPath(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var file model.AccountsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return file.Accounts, nil
}

func (s *Store) save(userID string, accounts []model.EmailAccount) error {
	path := s.accountsPath(userID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file := model.AccountsFile{Accounts: accounts}
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// splitEmail splits "user@domain.com" into ("domain.com", "user").
func splitEmail(email string) (domain, local string) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[1]), strings.ToLower(parts[0])
	}
	return "unknown", strings.ToLower(email)
}
