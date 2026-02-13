package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/auth"
	"github.com/eslider/mails/internal/model"
	"github.com/eslider/mails/internal/search/eml"
	"github.com/eslider/mails/internal/search/index"
	"github.com/eslider/mails/internal/sync"
	sync_pst "github.com/eslider/mails/internal/sync/pst"
	"github.com/eslider/mails/internal/user"
)

// --- Auth handlers ---

func handleLoginPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(loginHTML))
	}
}

func handleLoginSubmit(sessions *auth.SessionStore, users *user.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		email := strings.TrimSpace(r.FormValue("email"))
		password := r.FormValue("password")

		if email == "" || password == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(loginHTMLWithError("Email and password are required.")))
			return
		}

		u := users.FindByEmail(email)
		if u == nil || !auth.CheckPassword(u.PasswordHash, password) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(loginHTMLWithError("Invalid email or password.")))
			return
		}

		token, err := sessions.Create(u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session creation failed")
			return
		}

		auth.SetCookie(w, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleRegisterPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(registerHTML))
	}
}

func handleRegisterSubmit(sessions *auth.SessionStore, users *user.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		email := strings.TrimSpace(r.FormValue("email"))
		password := r.FormValue("password")
		confirm := r.FormValue("password_confirm")

		if name == "" || email == "" || password == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(registerHTMLWithError("All fields are required.")))
			return
		}

		if password != confirm {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(registerHTMLWithError("Passwords do not match.")))
			return
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(registerHTMLWithError(err.Error())))
			return
		}

		u, err := users.CreateWithPassword(name, email, hash)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(registerHTMLWithError(err.Error())))
			return
		}

		token, err := sessions.Create(u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session creation failed")
			return
		}

		auth.SetCookie(w, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleOAuthStart(providers *auth.Providers, sessions *auth.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		state := generateState()
		// Store state in cookie for CSRF validation.
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Path:     "/",
			MaxAge:   600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		url, err := providers.AuthURL(provider, state)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

func handleOAuthCallback(providers *auth.Providers, sessions *auth.SessionStore, users *user.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")

		// Validate state.
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			writeError(w, http.StatusBadRequest, "invalid oauth state")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeError(w, http.StatusBadRequest, "missing authorization code")
			return
		}

		oauthUser, err := providers.Exchange(r.Context(), provider, code)
		if err != nil {
			log.Printf("ERROR: oauth exchange %s: %v", provider, err)
			writeError(w, http.StatusInternalServerError, "authentication failed")
			return
		}

		// Find or create user.
		u, err := users.FindOrCreate(oauthUser)
		if err != nil {
			log.Printf("ERROR: create user: %v", err)
			writeError(w, http.StatusInternalServerError, "user creation failed")
			return
		}

		// Create session.
		token, err := sessions.Create(u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session creation failed")
			return
		}

		auth.SetCookie(w, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleLogout(sessions *auth.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := auth.TokenFromRequest(r)
		if token != "" {
			sessions.Delete(token)
		}
		auth.ClearCookie(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func handleDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
	}
}

// --- User API ---

func handleMe(users *user.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		u := users.Get(userID)
		if u == nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
}

// --- Account API ---

func handleListAccounts(accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		list, err := accounts.List(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []model.EmailAccount{}
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func handleCreateAccount(accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())

		var acct model.EmailAccount
		if err := json.NewDecoder(r.Body).Decode(&acct); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		created, err := accounts.Create(userID, acct)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	}
}

func handleUpdateAccount(accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		accountID := chi.URLParam(r, "id")

		var acct model.EmailAccount
		if err := json.NewDecoder(r.Body).Decode(&acct); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		acct.ID = accountID

		if err := accounts.Update(userID, acct); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, acct)
	}
}

func handleDeleteAccount(accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		accountID := chi.URLParam(r, "id")

		if err := accounts.Delete(userID, accountID); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// --- Sync API ---

func handleSyncTrigger(syncSvc *sync.Service, accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())

		var req struct {
			AccountID string `json:"account_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		var err error
		if req.AccountID != "" {
			err = syncSvc.SyncAccount(userID, req.AccountID)
		} else {
			err = syncSvc.SyncAll(userID)
		}

		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	}
}

func handleSyncStop(syncSvc *sync.Service, accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())

		var req struct {
			AccountID string `json:"account_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.AccountID == "" {
			writeError(w, http.StatusBadRequest, "account_id is required")
			return
		}

		// Verify the account belongs to the authenticated user.
		if _, err := accounts.Get(userID, req.AccountID); err != nil {
			writeError(w, http.StatusForbidden, "account not found or access denied")
			return
		}

		if err := syncSvc.StopSync(req.AccountID); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
	}
}

func handleSyncStatus(syncSvc *sync.Service, accounts *account.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		accts, err := accounts.List(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		statuses := make([]map[string]any, len(accts))
		for i, acct := range accts {
			statuses[i] = syncSvc.AccountStatus(userID, acct)
		}
		writeJSON(w, http.StatusOK, statuses)
	}
}

// --- Search API ---

func handleSearch(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		q := r.URL.Query().Get("q")
		accountID := r.URL.Query().Get("account_id")
		limit := queryInt(r, "limit", 50)
		offset := queryInt(r, "offset", 0)

		if limit < 1 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}

		// Get account to determine email dir.
		accts, _ := cfg.Accounts.List(userID)
		if len(accts) == 0 {
			writeJSON(w, http.StatusOK, index.SearchResult{Hits: []index.Hit{}})
			return
		}

		// If account_id specified, search that account; otherwise search all.
		var emailDir, indexPath string
		if accountID != "" {
			for _, a := range accts {
				if a.ID == accountID {
					emailDir = account.EmailDir(cfg.UsersDir, userID, a)
					indexPath = account.IndexPath(cfg.UsersDir, userID, a)
					break
				}
			}
		} else if len(accts) > 0 {
			// Default: first account.
			emailDir = account.EmailDir(cfg.UsersDir, userID, accts[0])
			indexPath = account.IndexPath(cfg.UsersDir, userID, accts[0])
		}

		if emailDir == "" {
			writeJSON(w, http.StatusOK, index.SearchResult{Hits: []index.Hit{}})
			return
		}

		idx, err := index.New(emailDir, indexPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "index error: "+err.Error())
			return
		}
		defer idx.Close()

		if idx.Stats().TotalEmails == 0 {
			idx.Build()
		}

		result := idx.Search(q, offset, limit)
		writeJSON(w, http.StatusOK, result)
	}
}

func handleEmailDetail(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		p := r.URL.Query().Get("path")
		accountID := r.URL.Query().Get("account_id")

		if p == "" {
			writeError(w, http.StatusBadRequest, "missing path parameter")
			return
		}

		cleaned := filepath.Clean(p)
		if strings.Contains(cleaned, "..") {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}

		accts, _ := cfg.Accounts.List(userID)
		var emailDir string
		if accountID != "" {
			for _, a := range accts {
				if a.ID == accountID {
					emailDir = account.EmailDir(cfg.UsersDir, userID, a)
					break
				}
			}
		} else if len(accts) > 0 {
			emailDir = account.EmailDir(cfg.UsersDir, userID, accts[0])
		}

		if emailDir == "" {
			writeError(w, http.StatusNotFound, "no accounts configured")
			return
		}

		full := filepath.Join(emailDir, cleaned)
		fe, err := eml.ParseFileFull(full)
		if err != nil {
			writeError(w, http.StatusNotFound, "email not found")
			return
		}
		fe.Path = cleaned
		writeJSON(w, http.StatusOK, fe)
	}
}

func handleSearchStats(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		accts, _ := cfg.Accounts.List(userID)

		out := map[string]any{
			"total_emails":         0,
			"accounts":             len(accts),
			"similarity_available": cfg.QdrantURL != "" && cfg.OllamaURL != "",
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func handleReindex(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		accts, err := cfg.Accounts.List(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		go func() {
			for _, acct := range accts {
				emailDir := account.EmailDir(cfg.UsersDir, userID, acct)
				indexPath := account.IndexPath(cfg.UsersDir, userID, acct)
				idx, err := index.New(emailDir, indexPath)
				if err != nil {
					log.Printf("WARN: reindex %s: %v", acct.Email, err)
					continue
				}
				idx.ClearCache()
				idx.Build()
				idx.Close()
				log.Printf("INFO: reindexed %s", acct.Email)
			}
		}()

		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	}
}

func handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Helpers ---

func queryInt(r *http.Request, key string, fallback int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return fallback
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return fallback
	}
	return v
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Embedded HTML ---

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Archive — Sign In</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body class="login-page">
<div class="login-container">
  <h1>Mail Archive</h1>
  <p class="login-subtitle">Sign in to your account</p>
  <form method="POST" action="/login" class="auth-form">
    <div class="form-group">
      <label for="email">Email</label>
      <input class="form-control" id="email" name="email" type="email" placeholder="you@example.com" required autofocus>
    </div>
    <div class="form-group">
      <label for="password">Password</label>
      <input class="form-control" id="password" name="password" type="password" placeholder="Your password" required>
    </div>
    <button type="submit" class="btn btn-primary" style="width:100%;margin-top:0.5rem">Sign In</button>
  </form>
  <p class="auth-switch">Don't have an account? <a href="/register">Create one</a></p>
  <a href="https://github.com/eSlider/mail-archive" target="_blank" rel="noopener" class="github-link" style="margin-top:1.5rem;display:inline-flex;align-items:center;gap:0.4rem;color:#6b7280;font-size:0.8rem" title="View on GitHub">
    <svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
    GitHub
  </a>
</div>
</body>
</html>`

func loginHTMLWithError(errMsg string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Archive — Sign In</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body class="login-page">
<div class="login-container">
  <h1>Mail Archive</h1>
  <p class="login-subtitle">Sign in to your account</p>
  <div class="auth-error">` + errMsg + `</div>
  <form method="POST" action="/login" class="auth-form">
    <div class="form-group">
      <label for="email">Email</label>
      <input class="form-control" id="email" name="email" type="email" placeholder="you@example.com" required autofocus>
    </div>
    <div class="form-group">
      <label for="password">Password</label>
      <input class="form-control" id="password" name="password" type="password" placeholder="Your password" required>
    </div>
    <button type="submit" class="btn btn-primary" style="width:100%;margin-top:0.5rem">Sign In</button>
  </form>
  <p class="auth-switch">Don't have an account? <a href="/register">Create one</a></p>
  <a href="https://github.com/eSlider/mail-archive" target="_blank" rel="noopener" class="github-link" style="margin-top:1.5rem;display:inline-flex;align-items:center;gap:0.4rem;color:#6b7280;font-size:0.8rem" title="View on GitHub">
    <svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
    GitHub
  </a>
</div>
</body>
</html>`
}

const registerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Archive — Register</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body class="login-page">
<div class="login-container">
  <h1>Mail Archive</h1>
  <p class="login-subtitle">Create your account</p>
  <form method="POST" action="/register" class="auth-form">
    <div class="form-group">
      <label for="name">Name</label>
      <input class="form-control" id="name" name="name" type="text" placeholder="Your name" required autofocus>
    </div>
    <div class="form-group">
      <label for="email">Email</label>
      <input class="form-control" id="email" name="email" type="email" placeholder="you@example.com" required>
    </div>
    <div class="form-group">
      <label for="password">Password</label>
      <input class="form-control" id="password" name="password" type="password" placeholder="Min. 6 characters" required minlength="6">
    </div>
    <div class="form-group">
      <label for="password_confirm">Confirm Password</label>
      <input class="form-control" id="password_confirm" name="password_confirm" type="password" placeholder="Repeat password" required minlength="6">
    </div>
    <button type="submit" class="btn btn-primary" style="width:100%;margin-top:0.5rem">Create Account</button>
  </form>
  <p class="auth-switch">Already have an account? <a href="/login">Sign in</a></p>
  <a href="https://github.com/eSlider/mail-archive" target="_blank" rel="noopener" class="github-link" style="margin-top:1.5rem;display:inline-flex;align-items:center;gap:0.4rem;color:#6b7280;font-size:0.8rem" title="View on GitHub">
    <svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
    GitHub
  </a>
</div>
</body>
</html>`

func registerHTMLWithError(errMsg string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Archive — Register</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body class="login-page">
<div class="login-container">
  <h1>Mail Archive</h1>
  <p class="login-subtitle">Create your account</p>
  <div class="auth-error">` + errMsg + `</div>
  <form method="POST" action="/register" class="auth-form">
    <div class="form-group">
      <label for="name">Name</label>
      <input class="form-control" id="name" name="name" type="text" placeholder="Your name" required autofocus>
    </div>
    <div class="form-group">
      <label for="email">Email</label>
      <input class="form-control" id="email" name="email" type="email" placeholder="you@example.com" required>
    </div>
    <div class="form-group">
      <label for="password">Password</label>
      <input class="form-control" id="password" name="password" type="password" placeholder="Min. 6 characters" required minlength="6">
    </div>
    <div class="form-group">
      <label for="password_confirm">Confirm Password</label>
      <input class="form-control" id="password_confirm" name="password_confirm" type="password" placeholder="Repeat password" required minlength="6">
    </div>
    <button type="submit" class="btn btn-primary" style="width:100%;margin-top:0.5rem">Create Account</button>
  </form>
  <p class="auth-switch">Already have an account? <a href="/login">Sign in</a></p>
  <a href="https://github.com/eSlider/mail-archive" target="_blank" rel="noopener" class="github-link" style="margin-top:1.5rem;display:inline-flex;align-items:center;gap:0.4rem;color:#6b7280;font-size:0.8rem" title="View on GitHub">
    <svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
    GitHub
  </a>
</div>
</body>
</html>`
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Archive</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body>
<div id="app"></div>
<script src="/static/js/vendor/jquery-3.7.1.min.js"></script>
<script src="/static/js/vendor/vue-3.5.13.global.prod.js"></script>
<script src="/static/js/app/main.js"></script>
</body>
</html>`

// --- PST/OST Import ---

// importJob tracks a running PST/OST import.
type importJob struct {
	ID        string `json:"id"`
	UserID    string `json:"-"` // owner; not exposed in JSON responses
	AccountID string `json:"account_id"`
	Filename  string `json:"filename"`
	Phase     string `json:"phase"`   // "uploading", "extracting", "indexing", "done", "error"
	Current   int    `json:"current"` // bytes uploaded or messages extracted
	Total     int    `json:"total"`   // total bytes or total messages
	Error     string `json:"error,omitempty"`
}

var importJobRetention = 10 * time.Minute

var (
	importJobsMu  gosync.Mutex
	importJobsMap = make(map[string]*importJob)
)

func setImportJob(jobID string, job *importJob) {
	importJobsMu.Lock()
	defer importJobsMu.Unlock()
	importJobsMap[jobID] = job
}

func getImportJob(jobID string) (*importJob, bool) {
	importJobsMu.Lock()
	defer importJobsMu.Unlock()
	job, ok := importJobsMap[jobID]
	return job, ok
}

func deleteImportJob(jobID string) {
	importJobsMu.Lock()
	defer importJobsMu.Unlock()
	delete(importJobsMap, jobID)
}

// scheduleImportJobCleanup removes a finished job from importJobsMap after
// importJobRetention so completed/failed jobs don't accumulate forever.
func scheduleImportJobCleanup(jobID string) {
	time.AfterFunc(importJobRetention, func() {
		importJobsMu.Lock()
		delete(importJobsMap, jobID)
		importJobsMu.Unlock()
	})
}

func handleImportPST(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())

		// Use MultipartReader to stream the upload directly to a temp file
		// without buffering the entire payload in memory or a temp file first.
		mr, mrErr := r.MultipartReader()
		if mrErr != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form: "+mrErr.Error())
			return
		}

		var title string
		var filename string
		var fileSize int64
		var filePart io.Reader
		for {
			part, partErr := mr.NextPart()
			if partErr == io.EOF {
				break
			}
			if partErr != nil {
				writeError(w, http.StatusBadRequest, "multipart read error: "+partErr.Error())
				return
			}
			switch part.FormName() {
			case "title":
				b, _ := io.ReadAll(part)
				title = strings.TrimSpace(string(b))
			case "file":
				filename = part.FileName()
				filePart = part
			}
			if filePart != nil {
				break // process file immediately — don't buffer remaining parts
			}
		}

		if filePart == nil {
			writeError(w, http.StatusBadRequest, "file is required")
			return
		}

		if title == "" {
			title = filename
		}
		// Sanitize title to prevent filesystem path traversal — strip
		// directory components and collapse any ".." sequences.
		title = filepath.Base(title)
		title = strings.ReplaceAll(title, "..", "")
		if title == "" || title == "." {
			title = "imported.pst"
		}

		// Use Content-Length as an estimate for progress reporting.
		fileSize = r.ContentLength

		// Create import job ID.
		jobID := model.NewID()
		job := &importJob{
			ID:       jobID,
			UserID:   userID,
			Filename: filename,
			Phase:    "uploading",
			Total:    int(fileSize),
		}

		importJobsMu.Lock()
		importJobsMap[jobID] = job
		importJobsMu.Unlock()

		onUploadProgress := func(phase string, current, total int) {
			importJobsMu.Lock()
			job.Phase = phase
			job.Current = current
			job.Total = total
			importJobsMu.Unlock()
		}

		// Stream upload directly to temp file (single copy, no intermediate buffer).
		tmpPath, uploadErr := sync_pst.StreamUpload(filePart, fileSize, onUploadProgress)
		if uploadErr != nil {
			importJobsMu.Lock()
			job.Phase = "error"
			job.Error = uploadErr.Error()
			importJobsMu.Unlock()
			scheduleImportJobCleanup(jobID)
			writeError(w, http.StatusInternalServerError, uploadErr.Error())
			return
		}

		// Create PST account.
		acct := model.EmailAccount{
			Type:    model.AccountTypePST,
			Email:   title,
			Folders: "all",
			Sync:    model.SyncConfig{Interval: "0", Enabled: false},
		}
		created, err := cfg.Accounts.Create(userID, acct)
		if err != nil {
			os.Remove(tmpPath)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		job.AccountID = created.ID

		// Run import in background.
		go func() {
			defer os.Remove(tmpPath)
			defer scheduleImportJobCleanup(jobID)

			emailDir := account.EmailDir(cfg.UsersDir, userID, *created)
			os.MkdirAll(emailDir, 0o755)

			onExtractProgress := func(phase string, current, total int) {
				importJobsMu.Lock()
				job.Phase = phase
				job.Current = current
				job.Total = total
				importJobsMu.Unlock()
			}

			extracted, errCount, importErr := sync_pst.Import(tmpPath, emailDir, onExtractProgress)
			if importErr != nil {
				importJobsMu.Lock()
				job.Phase = "error"
				job.Error = importErr.Error()
				importJobsMu.Unlock()
				log.Printf("ERROR: PST import %s: %v", filename, importErr)
				return
			}

			log.Printf("INFO: PST import %s: %d extracted, %d errors", filename, extracted, errCount)

			// Index the imported emails.
			importJobsMu.Lock()
			job.Phase = "indexing"
			job.Current = 0
			job.Total = extracted
			importJobsMu.Unlock()

			indexPath := account.IndexPath(cfg.UsersDir, userID, *created)
			idx, idxErr := index.New(emailDir, indexPath)
			if idxErr != nil {
				importJobsMu.Lock()
				job.Phase = "error"
				job.Error = "indexing failed: " + idxErr.Error()
				importJobsMu.Unlock()
				log.Printf("ERROR: index after PST import %s: %v", filename, idxErr)
				return
			}
			idx.ClearCache()
			idx.Build()
			idx.Close()

			importJobsMu.Lock()
			job.Phase = "done"
			job.Current = extracted
			job.Total = extracted
			importJobsMu.Unlock()
		}()

		writeJSON(w, http.StatusAccepted, map[string]any{
			"job_id":     jobID,
			"account_id": created.ID,
			"filename":   filename,
		})
	}
}

func handleImportStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		jobID := chi.URLParam(r, "id")

		importJobsMu.Lock()
		job, ok := importJobsMap[jobID]
		var snapshot importJob
		if ok {
			snapshot = *job
		}
		importJobsMu.Unlock()

		if !ok || snapshot.UserID != userID {
			writeError(w, http.StatusNotFound, "import job not found")
			return
		}

		writeJSON(w, http.StatusOK, snapshot)
	}
}
