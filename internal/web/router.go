// Package web provides the HTTP router and handlers for the mail application.
package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/auth"
	"github.com/eslider/mails/internal/sync"
	"github.com/eslider/mails/internal/user"
)

// Note: go:embed paths must be relative to the source file.
// Since static assets are in web/static/ (project root), we serve them via
// http.Dir at runtime instead of embedding.

// StaticDir is the path to static assets, set at startup.
var StaticDir string

// TemplateDir is the path to HTML templates, set at startup.
var TemplateDir string

// Config holds dependencies for the web layer.
type Config struct {
	Users    *user.Store
	Accounts *account.Store
	Sessions *auth.SessionStore
	Auth     *auth.Providers
	Sync     *sync.Service
	UsersDir string

	// Search (optional — per-user indices are loaded on demand).
	QdrantURL  string
	OllamaURL  string
	EmbedModel string
}

// NewRouter creates the Chi router with all routes.
func NewRouter(cfg Config) http.Handler {
	r := chi.NewRouter()

	// Middleware.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(corsMiddleware)

	// Static assets (jQuery, Vue.js, CSS, JS).
	if StaticDir != "" {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(StaticDir))))
	}

	// Public routes.
	r.Group(func(r chi.Router) {
		r.Get("/login", handleLoginPage())
		r.Post("/login", handleLoginSubmit(cfg.Sessions, cfg.Users))
		r.Get("/register", handleRegisterPage())
		r.Post("/register", handleRegisterSubmit(cfg.Sessions, cfg.Users))

		// OAuth (optional, only if providers are configured).
		r.Get("/auth/{provider}", handleOAuthStart(cfg.Auth, cfg.Sessions))
		r.Get("/auth/{provider}/callback", handleOAuthCallback(cfg.Auth, cfg.Sessions, cfg.Users))

		r.Get("/health", handleHealth())
	})

	// Protected routes (require authentication).
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(cfg.Sessions))

		// Pages.
		r.Get("/", handleDashboard())
		r.Post("/logout", handleLogout(cfg.Sessions))

		// User API.
		r.Get("/api/me", handleMe(cfg.Users))

		// Account API.
		r.Get("/api/accounts", handleListAccounts(cfg.Accounts))
		r.Post("/api/accounts", handleCreateAccount(cfg.Accounts))
		r.Put("/api/accounts/{id}", handleUpdateAccount(cfg.Accounts))
		r.Delete("/api/accounts/{id}", handleDeleteAccount(cfg.Accounts))

		// Sync API.
		r.Post("/api/sync", handleSyncTrigger(cfg.Sync, cfg.Accounts))
		r.Post("/api/sync/stop", handleSyncStop(cfg.Sync))
		r.Get("/api/sync/status", handleSyncStatus(cfg.Sync, cfg.Accounts))

		// Import API (PST/OST).
		r.Post("/api/import/pst", handleImportPST(cfg))
		r.Get("/api/import/status/{id}", handleImportStatus())

		// Search API.
		r.Get("/api/search", handleSearch(cfg))
		r.Get("/api/email", handleEmailDetail(cfg))
		r.Get("/api/stats", handleSearchStats(cfg))
		r.Post("/api/reindex", handleReindex(cfg))
	})

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
