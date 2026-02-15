package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/auth"
	"github.com/eslider/mails/internal/user"
)

func TestProtectedRoutesRequireAuth(t *testing.T) {
	dir := t.TempDir()
	sessions, err := auth.NewSessionStore(dir, nil)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	users, err := user.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	os.MkdirAll(dir, 0755)

	cfg := Config{
		Users:     users,
		Accounts:  account.NewStore(dir, nil),
		Sessions:  sessions,
		UsersDir:  dir,
		BlobStore: nil,
	}
	handler := NewRouter(cfg)

	tests := []struct {
		path string
	}{
		{"/api/email"},
		{"/api/email/download"},
		{"/api/email/attachment"},
		{"/api/email/cid"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path+"?path=x&cid=y", nil)
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("GET %s without auth: status = %d, want 401", tt.path, rec.Code)
			}
		})
	}
}
