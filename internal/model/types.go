// Package model defines core data types shared across the application.
package model

import (
	"time"

	"github.com/google/uuid"
)

// NewID generates a UUIDv7 (time-ordered) identifier.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to v4 if v7 fails (should never happen).
		return uuid.New().String()
	}
	return id.String()
}

// User represents a registered user.
type User struct {
	ID           string    `json:"id" yaml:"id"`
	Name         string    `json:"name" yaml:"name"`
	Email        string    `json:"email" yaml:"email"`
	AvatarURL    string    `json:"avatar_url,omitempty" yaml:"avatar_url,omitempty"`
	PasswordHash string    `json:"-" yaml:"password_hash,omitempty"`             // bcrypt hash, never exposed
	Provider     string    `json:"provider,omitempty" yaml:"provider,omitempty"` // "local", "github", "google", "facebook"
	ProviderID   string    `json:"provider_id,omitempty" yaml:"provider_id,omitempty"`
	CreatedAt    time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" yaml:"updated_at"`
}

// AccountType identifies the email protocol.
type AccountType string

const (
	AccountTypeIMAP     AccountType = "IMAP"
	AccountTypePOP3     AccountType = "POP3"
	AccountTypeGmailAPI AccountType = "GMAIL_API"
	AccountTypePST      AccountType = "PST"
)

// EmailAccount represents a single email account configuration.
type EmailAccount struct {
	ID       string      `json:"id" yaml:"id"`
	Type     AccountType `json:"type" yaml:"type"`
	Email    string      `json:"email" yaml:"email"`
	Host     string      `json:"host,omitempty" yaml:"host,omitempty"`
	Port     int         `json:"port,omitempty" yaml:"port,omitempty"`
	Password string      `json:"-" yaml:"password,omitempty"` // Never expose via JSON
	SSL      bool        `json:"ssl,omitempty" yaml:"ssl,omitempty"`
	Folders  string      `json:"folders,omitempty" yaml:"folders,omitempty"` // "all" or comma-separated

	Sync SyncConfig `json:"sync" yaml:"sync"`
}

// SyncConfig controls sync timing for an email account.
type SyncConfig struct {
	Interval string `json:"interval" yaml:"interval"` // e.g. "5m", "1h30m"
	Enabled  bool   `json:"enabled" yaml:"enabled"`
}

// AccountsFile is the per-user accounts.yml structure.
type AccountsFile struct {
	Accounts []EmailAccount `json:"accounts" yaml:"accounts"`
}

// SyncJob represents a sync task tracked in SQLite.
type SyncJob struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"account_id"`
	Status      SyncStatus `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	NewMessages int        `json:"new_messages"`
	Error       string     `json:"error,omitempty"`
}

// SyncStatus represents the state of a sync job.
type SyncStatus string

const (
	SyncStatusPending SyncStatus = "pending"
	SyncStatusRunning SyncStatus = "running"
	SyncStatusDone    SyncStatus = "done"
	SyncStatusFailed  SyncStatus = "failed"
)

// Session holds an authenticated user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SearchQuery represents a user search request.
type SearchQuery struct {
	Query  string `json:"q"`
	Mode   string `json:"mode"` // "keyword" or "similarity"
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}
