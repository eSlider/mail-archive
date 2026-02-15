package web

import (
	"context"
	"crypto/rand"
	"errors"
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
	"github.com/eslider/mails/internal/storage"
	"github.com/eslider/mails/internal/sync"
	sync_pst "github.com/eslider/mails/internal/sync/pst"
	"github.com/eslider/mails/internal/user"
)

// --- Auth handlers ---

func handleLoginPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = renderLogin(w, "")
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
			_ = renderLogin(w, "Email and password are required.")
			return
		}

		u := users.FindByEmail(email)
		if u == nil || !auth.CheckPassword(u.PasswordHash, password) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = renderLogin(w, "Invalid email or password.")
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
		_ = renderRegister(w, "")
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
			_ = renderRegister(w, "All fields are required.")
			return
		}

		if password != confirm {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = renderRegister(w, "Passwords do not match.")
			return
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = renderRegister(w, err.Error())
			return
		}

		u, err := users.CreateWithPassword(name, email, hash)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = renderRegister(w, err.Error())
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
		_ = renderDashboard(w)
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

		var statuses []map[string]any
		for _, acct := range accts {
			if acct.Type == model.AccountTypePST {
				// PST is import-only: return clean status, no last_error from old sync attempts.
				statuses = append(statuses, map[string]any{
					"id":          acct.ID,
					"name":        acct.Email,
					"type":        "PST",
					"syncing":     false,
					"import_only": true,
				})
				continue
			}
			statuses = append(statuses, syncSvc.AccountStatus(userID, acct))
		}
		writeJSON(w, http.StatusOK, statuses)
	}
}

// --- Search API ---

func handleSearch(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.UserIDFromContext(r.Context())
		q := r.URL.Query().Get("q")
		accountFilter := r.URL.Query().Get("account_id")
		accountIDsFilter := r.URL.Query().Get("account_ids")
		limit := queryInt(r, "limit", 50)
		offset := queryInt(r, "offset", 0)

		if limit < 1 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}

		accts, _ := cfg.Accounts.List(userID)
		if len(accts) == 0 {
			writeJSON(w, http.StatusOK, index.SearchResult{Hits: []index.Hit{}})
			return
		}

		var result index.SearchResult

		if accountFilter != "" {
			// Single account: search that account only.
			found := false
			for _, a := range accts {
				if a.ID == accountFilter {
					emailDir := account.EmailDir(cfg.UsersDir, userID, a)
					indexPath := account.IndexPath(cfg.UsersDir, userID, a)
					idx, err := index.New(emailDir, indexPath, cfg.BlobStore, cfg.UsersDir)
					if err != nil {
						writeError(w, http.StatusInternalServerError, "index error: "+err.Error())
						return
					}
					if idx.Stats().TotalEmails == 0 {
						idx.Build()
					}
					result = idx.Search(q, offset, limit)
					idx.Close()
					for i := range result.Hits {
						result.Hits[i].AccountID = a.ID
					}
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, http.StatusOK, index.SearchResult{Hits: []index.Hit{}})
				return
			}
		} else {
			// Filter by account_ids (comma-separated) if provided.
			var allowedIDs map[string]bool
			if accountIDsFilter != "" {
				allowedIDs = make(map[string]bool)
				for _, id := range strings.Split(accountIDsFilter, ",") {
					if id = strings.TrimSpace(id); id != "" {
						allowedIDs[id] = true
					}
				}
			}
			accountIndices := make([]index.AccountIndex, 0, len(accts))
			for _, a := range accts {
				if allowedIDs != nil && !allowedIDs[a.ID] {
					continue
				}
				accountIndices = append(accountIndices, index.AccountIndex{
					ID:        a.ID,
					IndexPath: account.IndexPath(cfg.UsersDir, userID, a),
				})
			}
			result = index.SearchMulti(accountIndices, q, offset, limit)
		}

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
		data, err := readEmailBytes(cfg, full)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "email not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to read email")
			return
		}
		fe, err := eml.ParseFileFullFromBytes(cleaned, data)
		if err != nil {
			writeError(w, http.StatusNotFound, "email not found")
			return
		}
		writeJSON(w, http.StatusOK, fe)
	}
}

// readEmailBytes returns email content by full path. Uses BlobStore when configured.
func readEmailBytes(cfg Config, fullPath string) ([]byte, error) {
	if cfg.BlobStore != nil {
		rel, err := filepath.Rel(cfg.UsersDir, fullPath)
		if err != nil {
			return nil, err
		}
		key := filepath.ToSlash(rel)
		return cfg.BlobStore.Read(context.Background(), key)
	}
	return os.ReadFile(fullPath)
}

// resolveEmailPath returns the full filesystem path for an email from path + account_id query params.
func resolveEmailPath(cfg Config, r *http.Request) (string, bool) {
	userID := auth.UserIDFromContext(r.Context())
	p := r.URL.Query().Get("path")
	accountID := r.URL.Query().Get("account_id")
	if p == "" {
		return "", false
	}
	cleaned := filepath.Clean(p)
	if strings.Contains(cleaned, "..") {
		return "", false
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
		return "", false
	}
	return filepath.Join(emailDir, cleaned), true
}

func handleEmailDownload(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		full, ok := resolveEmailPath(cfg, r)
		if !ok {
			writeError(w, http.StatusBadRequest, "missing or invalid path")
			return
		}
		data, err := readEmailBytes(cfg, full)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "email not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to read email")
			return
		}
		name := filepath.Base(full)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Content-Type", "message/rfc822")
		w.Write(data)
	}
}

func handleAttachmentDownload(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		full, ok := resolveEmailPath(cfg, r)
		if !ok {
			writeError(w, http.StatusBadRequest, "missing or invalid path")
			return
		}
		emailData, err := readEmailBytes(cfg, full)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "attachment not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to read email")
			return
		}
		idx := queryInt(r, "index", -1)
		if idx < 0 {
			writeError(w, http.StatusBadRequest, "missing or invalid index parameter")
			return
		}
		data, contentType, filename, err := eml.ExtractAttachmentFromBytes(emailData, idx)
		if err != nil {
			writeError(w, http.StatusNotFound, "attachment not found")
			return
		}
		if filename == "" {
			filename = "attachment"
		}
		// Escape quotes in filename to avoid breaking header
		safeName := strings.ReplaceAll(filename, `"`, `_`)
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeName+`"`)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write(data)
	}
}

// handleCIDResource serves inline MIME parts by Content-ID. Protected by RequireAuth;
// resolveEmailPath scopes access to the logged-in user's accounts only.
func handleCIDResource(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		full, ok := resolveEmailPath(cfg, r)
		if !ok {
			writeError(w, http.StatusBadRequest, "missing or invalid path")
			return
		}
		cid := strings.TrimSpace(r.URL.Query().Get("cid"))
		if cid == "" {
			writeError(w, http.StatusBadRequest, "missing cid parameter")
			return
		}
		data, contentType, err := eml.ExtractPartByCID(full, cid)
		if err != nil {
			writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(data)
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
				idx, err := index.New(emailDir, indexPath, cfg.BlobStore, cfg.UsersDir)
				if err != nil {
					log.Printf("WARN: reindex %s: %v", acct.Email, err)
					continue
				}
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

		// Run import in background via sync service.
		go func() {
			defer os.Remove(tmpPath)
			defer scheduleImportJobCleanup(jobID)

			onExtractProgress := func(phase string, current, total int) {
				importJobsMu.Lock()
				job.Phase = phase
				job.Current = current
				job.Total = total
				importJobsMu.Unlock()
			}

			extracted, errCount, importErr := cfg.Sync.ImportPST(userID, created.ID, tmpPath, onExtractProgress)
			if importErr != nil {
				importJobsMu.Lock()
				job.Phase = "error"
				job.Error = importErr.Error()
				importJobsMu.Unlock()
				log.Printf("ERROR: PST import %s: %v", filename, importErr)
				return
			}

			log.Printf("INFO: PST import %s: %d extracted, %d errors", filename, extracted, errCount)

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
