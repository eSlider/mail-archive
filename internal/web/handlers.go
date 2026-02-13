package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/auth"
	"github.com/eslider/mails/internal/model"
	"github.com/eslider/mails/internal/search/eml"
	"github.com/eslider/mails/internal/search/index"
	"github.com/eslider/mails/internal/sync"
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
		// TODO: implement background reindex per user/account.
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
