// Package user manages user storage under users/{id}/.
package user

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eslider/mails/internal/model"
)

const userMetaFile = "user.json"

// Store manages user data on the filesystem.
// Layout: {dataDir}/{userID}/user.json
type Store struct {
	mu      sync.RWMutex
	dataDir string
	// In-memory index: provider:providerID -> userID
	providerIndex map[string]string
	// In-memory index: email -> userID (for local auth)
	emailIndex map[string]string
	// In-memory index: userID -> User
	users map[string]model.User
}

// NewStore creates a user store, scanning existing users from disk.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create users dir: %w", err)
	}

	s := &Store{
		dataDir:       dataDir,
		providerIndex: make(map[string]string),
		emailIndex:    make(map[string]string),
		users:         make(map[string]model.User),
	}

	if err := s.loadAll(); err != nil {
		return nil, err
	}
	return s, nil
}

// FindOrCreate looks up a user by OAuth provider+ID. If not found, creates one.
func (s *Store) FindOrCreate(oauthUser *model.User) (*model.User, error) {
	key := providerKey(oauthUser.Provider, oauthUser.ProviderID)

	s.mu.RLock()
	if userID, ok := s.providerIndex[key]; ok {
		u := s.users[userID]
		s.mu.RUnlock()
		return &u, nil
	}
	s.mu.RUnlock()

	return s.create(oauthUser)
}

// Get returns a user by ID, or nil if not found.
func (s *Store) Get(id string) *model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.users[id]; ok {
		return &u
	}
	return nil
}

// FindByEmail returns a user by email address, or nil if not found.
func (s *Store) FindByEmail(email string) *model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if userID, ok := s.emailIndex[email]; ok {
		u := s.users[userID]
		return &u
	}
	return nil
}

// CreateWithPassword registers a new local user with username and password hash.
func (s *Store) CreateWithPassword(name, email, passwordHash string) (*model.User, error) {
	s.mu.RLock()
	if _, exists := s.emailIndex[email]; exists {
		s.mu.RUnlock()
		return nil, fmt.Errorf("email %q already registered", email)
	}
	s.mu.RUnlock()

	now := time.Now()
	user := model.User{
		ID:           model.NewID(),
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		Provider:     "local",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	userDir := s.UserDir(user.ID)
	for _, sub := range []string{"", "logs"} {
		if err := os.MkdirAll(filepath.Join(userDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create user dir: %w", err)
		}
	}

	if err := s.saveUser(user); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.users[user.ID] = user
	s.emailIndex[email] = user.ID
	s.mu.Unlock()

	return &user, nil
}

// UserDir returns the filesystem path for a user's data directory.
func (s *Store) UserDir(userID string) string {
	return filepath.Join(s.dataDir, userID)
}

func (s *Store) create(oauthUser *model.User) (*model.User, error) {
	now := time.Now()
	user := model.User{
		ID:         model.NewID(),
		Name:       oauthUser.Name,
		Email:      oauthUser.Email,
		AvatarURL:  oauthUser.AvatarURL,
		Provider:   oauthUser.Provider,
		ProviderID: oauthUser.ProviderID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	userDir := s.UserDir(user.ID)
	// Create user directory structure.
	for _, sub := range []string{"", "logs"} {
		if err := os.MkdirAll(filepath.Join(userDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create user dir: %w", err)
		}
	}

	if err := s.saveUser(user); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.users[user.ID] = user
	s.providerIndex[providerKey(user.Provider, user.ProviderID)] = user.ID
	s.mu.Unlock()

	return &user, nil
}

// userFile is the on-disk JSON representation that includes the password hash.
// The model.User struct hides PasswordHash from API responses (json:"-"),
// so we use this wrapper for persistence only.
type userFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	ProviderID   string    `json:"provider_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toUserFile(u model.User) userFile {
	return userFile{
		ID:           u.ID,
		Name:         u.Name,
		Email:        u.Email,
		AvatarURL:    u.AvatarURL,
		PasswordHash: u.PasswordHash,
		Provider:     u.Provider,
		ProviderID:   u.ProviderID,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}

func fromUserFile(f userFile) model.User {
	return model.User{
		ID:           f.ID,
		Name:         f.Name,
		Email:        f.Email,
		AvatarURL:    f.AvatarURL,
		PasswordHash: f.PasswordHash,
		Provider:     f.Provider,
		ProviderID:   f.ProviderID,
		CreatedAt:    f.CreatedAt,
		UpdatedAt:    f.UpdatedAt,
	}
}

func (s *Store) saveUser(u model.User) error {
	data, err := json.MarshalIndent(toUserFile(u), "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.UserDir(u.ID), userMetaFile)
	return os.WriteFile(path, data, 0o644)
}

func (s *Store) loadAll() error {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.dataDir, entry.Name(), userMetaFile)
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var f userFile
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		u := fromUserFile(f)
		s.users[u.ID] = u
		if u.Provider != "" && u.ProviderID != "" {
			s.providerIndex[providerKey(u.Provider, u.ProviderID)] = u.ID
		}
		if u.Email != "" {
			s.emailIndex[u.Email] = u.ID
		}
	}
	return nil
}

func providerKey(provider, providerID string) string {
	return provider + ":" + providerID
}
