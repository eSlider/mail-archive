package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eslider/mails/internal/model"
	"github.com/eslider/mails/internal/storage"
)

const (
	cookieName    = "mails_session"
	sessionMaxAge = 30 * 24 * time.Hour // 30 days
	sessionsFile  = "sessions.json"
)

// SessionStore manages user sessions backed by a JSON file or S3.
type SessionStore struct {
	mu        sync.RWMutex
	sessions  map[string]model.Session // token -> session
	dataDir   string
	blobStore storage.BlobStore
}

// NewSessionStore creates a session store. blobStore may be nil to use local filesystem.
func NewSessionStore(dataDir string, blobStore storage.BlobStore) (*SessionStore, error) {
	s := &SessionStore{
		sessions:  make(map[string]model.Session),
		dataDir:   dataDir,
		blobStore: blobStore,
	}
	if blobStore == nil {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, err
		}
	}
	s.load()
	return s, nil
}

// Create generates a new session for the user and returns the token.
func (s *SessionStore) Create(userID string) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	sess := model.Session{
		ID:        model.NewID(),
		UserID:    userID,
		Token:     token,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionMaxAge),
	}

	s.mu.Lock()
	s.sessions[token] = sess
	s.mu.Unlock()

	s.save()
	return token, nil
}

// Get returns the session for a token, or nil if not found or expired.
func (s *SessionStore) Get(token string) *model.Session {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil
	}
	if time.Now().After(sess.ExpiresAt) {
		s.Delete(token)
		return nil
	}
	return &sess
}

// Delete removes a session.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	s.save()
}

// SetCookie writes the session cookie to the response.
func SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // Set true behind TLS proxy
	})
}

// ClearCookie removes the session cookie.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// TokenFromRequest extracts the session token from cookie or Authorization header.
func TokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	// Support "Authorization: Bearer <token>" for API clients.
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *SessionStore) load() {
	if s.blobStore != nil {
		data, err := s.blobStore.Read(context.Background(), sessionsFile)
		if err != nil {
			return
		}
		var sessions []model.Session
		if err := json.Unmarshal(data, &sessions); err != nil {
			return
		}
		now := time.Now()
		for _, sess := range sessions {
			if now.Before(sess.ExpiresAt) {
				s.sessions[sess.Token] = sess
			}
		}
		return
	}
	path := filepath.Join(s.dataDir, sessionsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var sessions []model.Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return
	}
	now := time.Now()
	for _, sess := range sessions {
		if now.Before(sess.ExpiresAt) {
			s.sessions[sess.Token] = sess
		}
	}
}

func (s *SessionStore) save() {
	s.mu.RLock()
	sessions := make([]model.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return
	}
	if s.blobStore != nil {
		s.blobStore.Write(context.Background(), sessionsFile, data)
		return
	}
	path := filepath.Join(s.dataDir, sessionsFile)
	os.WriteFile(path, data, 0o600)
}
