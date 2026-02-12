// mail-search indexes .eml files and provides search across subject and body via CLI and HTTP.
//
// Usage:
//
//	mail-search search <query>           Search by subject and body (CLI)
//	mail-search serve                    Start HTTP server with search API + UI
//	mail-search stats                    Print index statistics
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/eslider/mails/search/eml"
	"github.com/eslider/mails/search/index"
	"github.com/eslider/mails/search/vector"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	emailDir := envOr("EMAILS_DIR", "../emails")
	listenAddr := envOr("LISTEN_ADDR", ":8080")
	indexPath := envOr("INDEX_PATH", "index.parquet")
	syncURL = envOr("SYNC_URL", "")
	qdrantURL := envOr("QDRANT_URL", "")
	ollamaURL := envOr("OLLAMA_URL", "")
	embedModel := envOr("EMBED_MODEL", "all-minilm")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	idx, err := index.New(emailDir, indexPath)
	if err != nil {
		log.Fatalf("Failed to create index: %v", err)
	}
	defer idx.Close()

	var vecStore *vector.Store
	if qdrantURL != "" && ollamaURL != "" {
		vs, err := vector.NewStore(qdrantURL, ollamaURL, embedModel)
		if err != nil {
			log.Printf("WARN: vector search unavailable (Qdrant/Ollama): %v", err)
		} else {
			defer vs.Close()
			vecStore = vs
			log.Printf("Similarity search enabled (Qdrant + Ollama %s)", embedModel)
		}
	}

	if idx.Stats().TotalEmails == 0 {
		log.Printf("Indexing emails from %s ...", emailDir)
		total, errCount := idx.Build()
		log.Printf("Indexed %d emails (%d errors)", total, errCount)
		if vecStore != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			vTotal, _, vErr := vecStore.IndexEmails(ctx, emailDir, index.WalkEmails, nil)
			cancel()
			if vErr != nil {
				log.Printf("WARN: vector index: %v", vErr)
			} else {
				log.Printf("Vector index: %d emails", vTotal)
			}
		}
	}

	switch os.Args[1] {
	case "search":
		runSearch(idx, vecStore, os.Args[2:])
	case "serve":
		runServer(idx, vecStore, listenAddr)
	case "stats":
		runStats(idx)
	default:
		printUsage()
		os.Exit(1)
	}
}

// syncURL is the base URL of the mail-sync trigger API (e.g. "http://mail-sync:8081").
var syncURL string

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: mail-search <command> [args]

Commands:
  search <query>   Search emails by subject and body (case-insensitive substring)
  serve            Start HTTP server (API + web UI)
  stats            Show index statistics

Environment:
  EMAILS_DIR       Path to emails directory (default: ../emails)
  INDEX_PATH       Path to Parquet index file (default: index.parquet)
  LISTEN_ADDR      HTTP listen address (default: :8080)
  QDRANT_URL       Qdrant gRPC address for similarity search (e.g. qdrant:6334)
  OLLAMA_URL       Ollama API (e.g. http://172.17.0.1:11434 for host from Docker)
  EMBED_MODEL      Ollama embedding model (default: all-minilm)`)
}

// --- CLI search ---

func runSearch(idx *index.Index, vecStore *vector.Store, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: mail-search search <query>")
		os.Exit(1)
	}
	query := strings.Join(args, " ")
	var result index.SearchResult
	if vecStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hits, total, err := vecStore.Search(ctx, query, 200, 0)
		if err != nil {
			log.Printf("WARN: similarity search failed: %v, falling back to keyword", err)
			result = idx.Search(query, 0, 0)
		} else {
			result = vectorResultsToSearchResult(query, hits, total, 0, 200)
		}
	} else {
		result = idx.Search(query, 0, 0)
	}

	if result.Total == 0 {
		fmt.Printf("No emails matching %q\n", query)
		return
	}

	fmt.Printf("Found %d email(s) matching %q:\n\n", result.Total, query)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "DATE\tFROM\tSUBJECT\tPATH\n")
	fmt.Fprintf(tw, "----\t----\t-------\t----\n")
	for _, h := range result.Hits {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			h.Date.Format("2006-01-02 15:04"),
			truncate(h.From, 30),
			truncate(h.Subject, 60),
			h.Path,
		)
		if h.Snippet != "" && !strings.Contains(strings.ToLower(h.Subject), strings.ToLower(query)) {
			fmt.Fprintf(tw, "\t\t  ↳ %s\t\n", truncate(h.Snippet, 80))
		}
	}
	tw.Flush()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// vectorResultsToSearchResult converts vector.SearchResult to index.SearchResult.
func vectorResultsToSearchResult(query string, hits []vector.SearchResult, total, offset, limit int) index.SearchResult {
	idxHits := make([]index.Hit, len(hits))
	for i, h := range hits {
		idxHits[i] = index.Hit{
			Email: eml.Email{
				Path:    h.Path,
				Subject: h.Subject,
				From:    h.From,
				To:      h.To,
				Date:    time.Unix(h.Date, 0),
			},
			Snippet: "",
		}
	}
	return index.SearchResult{
		Query:   query,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		Hits:    idxHits,
		IndexAt: time.Now(),
	}
}

// --- CLI stats ---

func runStats(idx *index.Index) {
	s := idx.Stats()
	fmt.Printf("Email directory:  %s\n", s.EmailDir)
	fmt.Printf("Index path:       %s\n", s.IndexPath)
	fmt.Printf("Total indexed:    %d\n", s.TotalEmails)
	fmt.Printf("Indexed at:       %s\n", s.IndexedAt.Format(time.RFC3339))
}

// --- HTTP server ---

func runServer(idx *index.Index, vecStore *vector.Store, addr string) {
	mux := http.NewServeMux()
	reindexState := &reindexProgress{}

	// API endpoints.
	mux.HandleFunc("GET /api/search", handleSearch(idx, vecStore))
	mux.HandleFunc("GET /api/email", handleEmail(idx))
	mux.HandleFunc("GET /api/stats", handleStats(idx, vecStore))
	mux.HandleFunc("POST /api/reindex", handleReindex(idx, vecStore, reindexState))
	mux.HandleFunc("GET /api/reindex/status", handleReindexStatus(reindexState))
	mux.HandleFunc("GET /health", handleHealth(idx))

	// Sync proxy endpoints (forwarded to mail-sync container).
	mux.HandleFunc("GET /api/accounts", handleSyncProxy("/accounts"))
	mux.HandleFunc("POST /api/sync", handleSyncProxy("/sync"))

	// Web UI.
	mux.HandleFunc("GET /", handleUI())

	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleSearch(idx *index.Index, vecStore *vector.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mode := r.URL.Query().Get("mode")
		limit := queryInt(r, "limit", 50)
		offset := queryInt(r, "offset", 0)
		if limit < 1 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
		if offset < 0 {
			offset = 0
		}

		var result index.SearchResult
		if mode == "similarity" && vecStore != nil && strings.TrimSpace(q) != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			hits, total, err := vecStore.Search(ctx, q, limit, offset)
			if err != nil {
				log.Printf("WARN: similarity search failed: %v, falling back to keyword", err)
				result = idx.Search(q, offset, limit)
			} else {
				result = vectorResultsToSearchResult(q, hits, total, offset, limit)
			}
		} else {
			result = idx.Search(q, offset, limit)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func handleEmail(idx *index.Index) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "" {
			http.Error(w, `{"error":"missing path parameter"}`, http.StatusBadRequest)
			return
		}
		// Prevent directory traversal.
		cleaned := filepath.Clean(p)
		if strings.Contains(cleaned, "..") {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		full := filepath.Join(idx.EmailDir(), cleaned)
		fe, err := eml.ParseFileFull(full)
		if err != nil {
			http.Error(w, `{"error":"email not found"}`, http.StatusNotFound)
			return
		}
		fe.Path = cleaned
		writeJSON(w, http.StatusOK, fe)
	}
}

func handleStats(idx *index.Index, vecStore *vector.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := idx.Stats()
		out := map[string]any{
			"total_emails":        s.TotalEmails,
			"indexed_at":          s.IndexedAt,
			"email_dir":           s.EmailDir,
			"index_path":          s.IndexPath,
			"similarity_available": vecStore != nil,
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// reindexProgress holds state for background reindex; safe for concurrent access.
type reindexProgress struct {
	mu           sync.RWMutex
	Running      bool    `json:"running"`
	Phase        string  `json:"phase"` // "parquet", "vector"
	ParquetTotal int     `json:"parquet_total"`
	VecIndexed   int     `json:"vec_indexed"`
	VecTotal     int     `json:"vec_total"`
	Error        string  `json:"error,omitempty"`
	Indexed      int     `json:"indexed"`
	Errors       int     `json:"errors"`
	ParquetSecs  float64 `json:"parquet_secs"`
	VectorSecs   float64 `json:"vector_secs"`
	TotalSecs    float64 `json:"total_secs"`
}

func handleReindex(idx *index.Index, vecStore *vector.Store, state *reindexProgress) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.Running {
			state.mu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]string{"error": "reindex already in progress"})
			return
		}
		state.Running = true
		state.Phase = "parquet"
		state.ParquetTotal = 0
		state.VecIndexed = 0
		state.VecTotal = 0
		state.Error = ""
		state.mu.Unlock()

		writeJSON(w, http.StatusAccepted, map[string]any{"status": "started"})

		go func() {
			start := time.Now()
			var total, errCount int
			var vecIndexed int
			var parquetSecs, vectorSecs float64

			defer func() {
				state.mu.Lock()
				state.Running = false
				state.Phase = ""
				state.Indexed = total
				state.VecIndexed = vecIndexed
				state.Errors = errCount
				state.ParquetSecs = parquetSecs
				state.VectorSecs = vectorSecs
				state.TotalSecs = time.Since(start).Seconds()
				state.mu.Unlock()
			}()

			total, errCount = idx.Build()
			parquetSecs = time.Since(start).Seconds()
			state.mu.Lock()
			state.ParquetTotal = total
			state.Phase = "vector"
			state.mu.Unlock()
			log.Printf("Parquet index: %d emails in %.1fs (%.1f emails/sec)", total, parquetSecs, float64(total)/parquetSecs)

			if vecStore != nil {
				vecStart := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
				defer cancel()
				progress := func(indexed, total int) {
					state.mu.Lock()
					state.VecIndexed = indexed
					state.VecTotal = total
					state.mu.Unlock()
				}
				vTotal, _, vErr := vecStore.IndexEmails(ctx, idx.EmailDir(), index.WalkEmails, progress)
				vectorSecs = time.Since(vecStart).Seconds()
				if vErr != nil {
					log.Printf("WARN: vector reindex: %v", vErr)
					state.mu.Lock()
					state.Error = vErr.Error()
					state.mu.Unlock()
				} else {
					vecIndexed = vTotal
				}
			}
		}()
	}
}

func handleReindexStatus(state *reindexProgress) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		out := map[string]any{
			"running":       state.Running,
			"phase":         state.Phase,
			"parquet_total": state.ParquetTotal,
			"vec_indexed":   state.VecIndexed,
			"vec_total":     state.VecTotal,
			"error":         state.Error,
			"indexed":       state.Indexed,
			"errors":        state.Errors,
			"parquet_secs":  state.ParquetSecs,
			"vector_secs":   state.VectorSecs,
			"total_secs":    state.TotalSecs,
		}
		state.mu.RUnlock()
		writeJSON(w, http.StatusOK, out)
	}
}

func handleHealth(idx *index.Index) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := idx.Stats()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "ok",
			"total_emails": s.TotalEmails,
			"indexed_at":   s.IndexedAt,
		})
	}
}

// handleSyncProxy forwards requests to the mail-sync trigger API.
func handleSyncProxy(targetPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if syncURL == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "sync service not configured (SYNC_URL not set)",
			})
			return
		}

		targetURL := syncURL + targetPath
		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "proxy request failed"})
			return
		}
		proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(proxyReq)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "sync service unavailable",
			})
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func handleUI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(uiHTML))
	}
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mail Search</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  :root {
    --bg: #0f1117;
    --surface: #1a1d27;
    --surface-2: #22252f;
    --border: #2a2d3a;
    --text: #e4e4e7;
    --text-dim: #9ca3af;
    --accent: #6366f1;
    --accent-light: #818cf8;
    --radius: 10px;
  }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    background: var(--bg);
    color: var(--text);
    min-height: 100vh;
  }
  a { color: var(--accent-light); text-decoration: none; }
  a:hover { text-decoration: underline; }

  /* --- Header --- */
  .header {
    border-bottom: 1px solid var(--border);
    padding: 1rem 2rem;
    display: flex;
    align-items: center;
    gap: 1rem;
    background: var(--surface);
    position: sticky; top: 0; z-index: 10;
  }
  .header h1 { font-size: 1.15rem; font-weight: 600; letter-spacing: -0.02em; cursor: pointer; }
  .header .pill {
    font-size: 0.72rem; padding: 0.2rem 0.55rem;
    background: var(--accent); border-radius: 999px; color: #fff; font-weight: 500;
  }
  .header .right-group { margin-left: auto; display: flex; align-items: center; gap: 0.75rem; }
  .header .stats { font-size: 0.78rem; color: var(--text-dim); }

  /* --- Search view --- */
  .view { display: none; }
  .view.active { display: block; }
  .container { max-width: 960px; margin: 0 auto; padding: 1.5rem; }
  .search-box { position: relative; margin-bottom: 1.25rem; }
  .search-box input {
    width: 100%; padding: 0.8rem 1rem 0.8rem 2.8rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text); font-size: 0.95rem;
    outline: none; transition: border-color 0.2s;
  }
  .search-box input:focus { border-color: var(--accent); }
  .search-box input::placeholder { color: var(--text-dim); }
  .search-box svg {
    position: absolute; left: 0.85rem; top: 50%; transform: translateY(-50%);
    color: var(--text-dim); width: 17px; height: 17px;
  }
  .meta {
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: 0.6rem; font-size: 0.82rem; color: var(--text-dim);
  }
  .btn-sm {
    background: none; border: 1px solid var(--border); color: var(--text-dim);
    padding: 0.25rem 0.7rem; border-radius: 6px; cursor: pointer; font-size: 0.78rem;
  }
  .btn-sm:hover { border-color: var(--accent); color: var(--text); }
  .mode-toggle {
    display: flex; gap: 0; border-radius: 6px; overflow: hidden;
    border: 1px solid var(--border);
  }
  .mode-toggle button {
    background: none; border: none; color: var(--text-dim);
    padding: 0.25rem 0.6rem; font-size: 0.75rem; cursor: pointer;
    transition: all 0.15s;
  }
  .mode-toggle button:hover { color: var(--text); }
  .mode-toggle button.active {
    background: var(--accent); color: #fff; font-weight: 500;
  }
  .results { display: flex; flex-direction: column; gap: 0.4rem; }
  .email-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.9rem 1.1rem;
    transition: border-color 0.15s, transform 0.1s; cursor: pointer;
    text-decoration: none; color: inherit; display: block;
  }
  .email-card:hover { border-color: var(--accent-light); transform: translateY(-1px); text-decoration: none; }
  .email-subject { font-weight: 600; font-size: 0.92rem; margin-bottom: 0.3rem; line-height: 1.4; }
  .email-subject mark, .email-snippet mark {
    background: rgba(99,102,241,0.3); color: var(--accent-light);
    border-radius: 2px; padding: 0 2px;
  }
  .email-meta {
    display: flex; gap: 1.2rem; font-size: 0.78rem; color: var(--text-dim); flex-wrap: wrap;
  }
  .email-meta span { white-space: nowrap; }
  .email-snippet {
    font-size: 0.8rem; color: var(--text-dim); margin-top: 0.25rem; line-height: 1.45;
    overflow: hidden; text-overflow: ellipsis;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
  }
  .empty { text-align: center; padding: 4rem 1rem; color: var(--text-dim); }
  .empty svg { width: 48px; height: 48px; margin-bottom: 1rem; opacity: 0.4; }
  .pager {
    display: flex; align-items: center; justify-content: center;
    gap: 0.35rem; margin-top: 1.25rem; flex-wrap: wrap;
  }
  .pager button {
    background: var(--surface); border: 1px solid var(--border); color: var(--text-dim);
    padding: 0.35rem 0.7rem; border-radius: 6px; cursor: pointer; font-size: 0.82rem;
    min-width: 2.2rem; text-align: center; transition: all 0.15s;
  }
  .pager button:hover:not(:disabled) { border-color: var(--accent); color: var(--text); }
  .pager button:disabled { opacity: 0.35; cursor: default; }
  .pager button.active { background: var(--accent); border-color: var(--accent); color: #fff; font-weight: 600; }
  .pager .pager-info { font-size: 0.78rem; color: var(--text-dim); margin: 0 0.5rem; }
  .spinner {
    display: inline-block; width: 16px; height: 16px;
    border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.6s linear infinite;
  }
  .spinner-lg {
    display: block; width: 32px; height: 32px; margin: 4rem auto;
    border: 3px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.6s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* --- Sync dropdown --- */
  .sync-wrap { position: relative; }
  .sync-btn {
    background: none; border: 1px solid var(--border); color: var(--text-dim);
    padding: 0.3rem 0.7rem; border-radius: 6px; cursor: pointer; font-size: 0.78rem;
    display: flex; align-items: center; gap: 0.35rem; white-space: nowrap;
    transition: all 0.15s;
  }
  .sync-btn:hover { border-color: var(--accent); color: var(--text); }
  .sync-btn svg { width: 14px; height: 14px; }
  .sync-dropdown {
    display: none; position: absolute; right: 0; top: calc(100% + 6px);
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); min-width: 300px; z-index: 100;
    box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  }
  .sync-dropdown.open { display: block; }
  .sync-dd-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.75rem 1rem; border-bottom: 1px solid var(--border);
  }
  .sync-dd-header span { font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .btn-accent {
    background: var(--accent); border: none; color: #fff;
    padding: 0.3rem 0.75rem; border-radius: 6px; cursor: pointer; font-size: 0.78rem;
    font-weight: 500; transition: opacity 0.15s;
  }
  .btn-accent:hover { opacity: 0.85; }
  .btn-accent:disabled { opacity: 0.5; cursor: default; }
  .sync-account-list { max-height: 320px; overflow-y: auto; }
  .sync-account {
    display: flex; align-items: center; gap: 0.6rem;
    padding: 0.6rem 1rem; border-bottom: 1px solid var(--border);
    font-size: 0.82rem;
  }
  .sync-account:last-child { border-bottom: none; }
  .sync-account-info { flex: 1; min-width: 0; }
  .sync-account-name { font-weight: 500; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sync-account-type {
    font-size: 0.7rem; color: var(--text-dim); background: var(--surface-2);
    padding: 0.1rem 0.4rem; border-radius: 3px; display: inline-block; margin-top: 0.15rem;
  }
  .sync-account-status { font-size: 0.72rem; color: var(--text-dim); white-space: nowrap; }
  .sync-account-status.syncing { color: var(--accent-light); }
  .sync-account-status.error { color: #ef4444; }
  .sync-account-status.done { color: #22c55e; }
  .sync-account-btn {
    background: none; border: 1px solid var(--border); color: var(--text-dim);
    padding: 0.2rem 0.5rem; border-radius: 5px; cursor: pointer; font-size: 0.72rem;
    white-space: nowrap; transition: all 0.15s; flex-shrink: 0;
  }
  .sync-account-btn:hover:not(:disabled) { border-color: var(--accent); color: var(--text); }
  .sync-account-btn:disabled { opacity: 0.35; cursor: default; }
  .sync-dd-footer {
    padding: 0.6rem 1rem; border-top: 1px solid var(--border);
    font-size: 0.75rem; color: var(--text-dim); text-align: center;
    display: none;
  }
  .sync-dd-footer.visible { display: block; }
  .reindex-progress-wrap {
    margin-top: 0.75rem; padding: 0.5rem 0.75rem;
    background: var(--surface-2); border: 1px solid var(--border);
    border-radius: var(--radius); font-size: 0.82rem; color: var(--text-dim);
  }
  .reindex-progress-wrap .reindex-progress-bar { margin-top: 0.35rem; }
  .reindex-progress-bar {
    height: 4px; background: var(--surface-2); border-radius: 2px;
    overflow: hidden; margin-top: 0.35rem;
  }
  .reindex-progress-bar .fill {
    display: block; height: 100%; background: var(--accent);
    border-radius: 2px; transition: width 0.2s;
  }

  /* --- Detail view --- */
  .detail-container { max-width: 960px; margin: 0 auto; padding: 1.5rem; }
  .back-link {
    display: inline-flex; align-items: center; gap: 0.4rem;
    font-size: 0.85rem; color: var(--text-dim); margin-bottom: 1.25rem; cursor: pointer;
  }
  .back-link:hover { color: var(--text); }
  .back-link svg { width: 16px; height: 16px; }
  .detail-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); overflow: hidden;
  }
  .detail-header { padding: 1.25rem 1.5rem; border-bottom: 1px solid var(--border); }
  .detail-subject { font-size: 1.15rem; font-weight: 700; line-height: 1.4; margin-bottom: 0.9rem; }
  .detail-fields { display: grid; grid-template-columns: auto 1fr; gap: 0.3rem 1rem; font-size: 0.85rem; }
  .detail-fields dt { color: var(--text-dim); font-weight: 500; white-space: nowrap; }
  .detail-fields dd { color: var(--text); word-break: break-word; }
  .detail-body { position: relative; min-height: 200px; }
  .detail-body iframe {
    width: 100%; border: none; display: block;
    min-height: 400px; background: #fff;
  }
  .detail-body-text {
    padding: 1.25rem 1.5rem; white-space: pre-wrap; font-size: 0.88rem;
    line-height: 1.65; color: var(--text); font-family: inherit;
  }
  .detail-attachments {
    border-top: 1px solid var(--border); padding: 1rem 1.5rem;
  }
  .detail-attachments h3 {
    font-size: 0.82rem; color: var(--text-dim); font-weight: 600;
    margin-bottom: 0.5rem; text-transform: uppercase; letter-spacing: 0.04em;
  }
  .att-list { list-style: none; display: flex; flex-wrap: wrap; gap: 0.5rem; }
  .att-item {
    background: var(--surface-2); border: 1px solid var(--border);
    border-radius: 6px; padding: 0.4rem 0.75rem; font-size: 0.8rem;
    display: flex; align-items: center; gap: 0.4rem;
  }
  .att-item svg { width: 14px; height: 14px; color: var(--text-dim); }
  .att-size { color: var(--text-dim); }
  .tab-bar {
    display: flex; border-bottom: 1px solid var(--border);
    background: var(--surface-2);
  }
  .tab-bar button {
    background: none; border: none; color: var(--text-dim);
    padding: 0.6rem 1.25rem; font-size: 0.82rem; cursor: pointer;
    border-bottom: 2px solid transparent; transition: all 0.15s;
  }
  .tab-bar button:hover { color: var(--text); }
  .tab-bar button.active {
    color: var(--accent-light); border-bottom-color: var(--accent);
  }

  @media (max-width: 640px) {
    .header { padding: 0.8rem 1rem; }
    .container, .detail-container { padding: 1rem; }
    .detail-header { padding: 1rem; }
    .detail-body-text { padding: 1rem; }
  }
</style>
</head>
<body>
  <div class="header">
    <h1 id="logo">Mail Search</h1>
    <span class="pill" id="total-pill">...</span>
    <div class="right-group">
      <span class="stats" id="index-info"></span>
      <div class="sync-wrap">
        <button class="sync-btn" id="sync-btn">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182M20.015 4.356v4.992"/></svg>
          Sync
        </button>
        <div class="sync-dropdown" id="sync-dropdown">
          <div class="sync-dd-header">
            <span>Sync Accounts</span>
            <button class="btn-accent" id="sync-all-btn">Sync All</button>
          </div>
          <div id="sync-accounts" class="sync-account-list"></div>
          <div class="sync-dd-footer" id="sync-footer"></div>
        </div>
      </div>
    </div>
  </div>

  <!-- Search list view -->
  <div id="search-view" class="view active">
    <div class="container">
      <div class="search-box">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><circle cx="11" cy="11" r="8"/><path stroke-linecap="round" d="m21 21-4.35-4.35"/></svg>
        <input type="text" id="q" placeholder="Search emails by subject and body..." autofocus>
      </div>
      <div class="meta">
        <span style="display:flex;align-items:center;gap:0.6rem">
          <span id="result-count"></span>
          <span class="mode-toggle" id="mode-toggle" style="display:none">
            <button type="button" data-mode="keyword" id="mode-keyword">Keyword</button>
            <button type="button" data-mode="similarity" id="mode-similarity">Similarity</button>
          </span>
        </span>
        <button class="btn-sm" id="reindex-btn">Reindex</button>
      </div>
      <div id="reindex-progress" class="reindex-progress-wrap" style="display:none">
        <span id="reindex-progress-text"></span>
        <div class="reindex-progress-bar"><div class="fill" id="reindex-progress-fill" style="width:0%"></div></div>
      </div>
      <div id="results" class="results"></div>
      <div id="pager" class="pager"></div>
    </div>
  </div>

  <!-- Email detail view -->
  <div id="detail-view" class="view">
    <div class="detail-container">
      <div class="back-link" id="back-btn">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 19.5 8.25 12l7.5-7.5"/></svg>
        Back to results
      </div>
      <div id="detail-content"></div>
    </div>
  </div>

<script>
(function() {
  var qInput = document.getElementById('q');
  var resultsDiv = document.getElementById('results');
  var pagerDiv = document.getElementById('pager');
  var countSpan = document.getElementById('result-count');
  var totalPill = document.getElementById('total-pill');
  var indexInfo = document.getElementById('index-info');
  var reindexBtn = document.getElementById('reindex-btn');
  var reindexProgress = document.getElementById('reindex-progress');
  var reindexProgressText = document.getElementById('reindex-progress-text');
  var reindexProgressFill = document.getElementById('reindex-progress-fill');
  var searchView = document.getElementById('search-view');
  var detailView = document.getElementById('detail-view');
  var detailContent = document.getElementById('detail-content');
  var backBtn = document.getElementById('back-btn');
  var logo = document.getElementById('logo');
  var debounceTimer;

  var PAGE_SIZE = 50;
  var currentOffset = 0;
  var currentQuery = '';
  var searchMode = 'keyword';
  var similarityAvailable = false;

  // --- Routing ---
  function navigate(hash) {
    if (location.hash !== hash) location.hash = hash;
    else route();
  }

  function route() {
    var h = location.hash || '#';
    if (h.startsWith('#/email/')) {
      var emailPath = decodeURIComponent(h.slice(8));
      showDetail(emailPath);
    } else {
      showSearch();
    }
  }

  function showSearch() {
    searchView.classList.add('active');
    detailView.classList.remove('active');
    document.title = 'Mail Search';
  }

  function showDetail(emailPath) {
    searchView.classList.remove('active');
    detailView.classList.add('active');
    loadEmail(emailPath);
  }

  logo.addEventListener('click', function() { navigate('#'); });
  backBtn.addEventListener('click', function() { history.back(); });
  window.addEventListener('hashchange', route);

  // --- Stats ---
  async function loadStats() {
    try {
      var r = await fetch('/api/stats');
      var s = await r.json();
      totalPill.textContent = s.total_emails + ' emails';
      if (s.indexed_at) {
        var d = new Date(s.indexed_at);
        indexInfo.textContent = 'Indexed ' + d.toLocaleString();
      }
      similarityAvailable = s.similarity_available === true;
      var mt = document.getElementById('mode-toggle');
      if (mt) mt.style.display = similarityAvailable ? 'flex' : 'none';
    } catch(e) {}
  }

  // --- Search ---
  function goToPage(page) {
    currentOffset = page * PAGE_SIZE;
    doSearch(currentQuery, currentOffset);
    window.scrollTo({top: 0, behavior: 'smooth'});
  }

  async function doSearch(query, offset) {
    if (typeof offset === 'undefined') offset = 0;
    currentQuery = query;
    currentOffset = offset;
    try {
      var url = '/api/search?limit=' + PAGE_SIZE + '&offset=' + offset + '&q=' + encodeURIComponent(query);
      if (searchMode === 'similarity') url += '&mode=similarity';
      var r = await fetch(url);
      var data = await r.json();
      renderList(data, query);
      renderPager(data);
    } catch(e) {
      resultsDiv.innerHTML = '<div class="empty"><p>Error fetching results.</p></div>';
      pagerDiv.innerHTML = '';
    }
  }

  function renderList(data, query) {
    var total = (typeof data.total === 'number') ? data.total : 0;
    var offset = (typeof data.offset === 'number') ? data.offset : 0;
    var hits = data.hits || [];
    var start = offset + 1;
    var end = offset + hits.length;
    if (total === 0) {
      countSpan.textContent = '0 results';
    } else {
      countSpan.textContent = start + '–' + end + ' of ' + total + ' result' + (total !== 1 ? 's' : '');
    }
    var items = hits;
    if (items.length === 0) {
      resultsDiv.innerHTML = '<div class="empty"><svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M21.75 9v.906a2.25 2.25 0 0 1-1.183 1.981l-6.478 3.488M2.25 9v.906a2.25 2.25 0 0 0 1.183 1.981l6.478 3.488m8.839 2.51-4.66-2.51m0 0-1.023-.55a2.25 2.25 0 0 0-2.134 0l-1.022.55m0 0-4.661 2.51m16.5 1.615a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V8.844a2.25 2.25 0 0 1 1.183-1.981l7.5-4.039a2.25 2.25 0 0 1 2.134 0l7.5 4.039a2.25 2.25 0 0 1 1.183 1.98V19.5Z"/></svg><p>No emails found' + (query ? ' for "' + esc(query) + '"' : '') + '</p></div>';
      pagerDiv.innerHTML = '';
      return;
    }
    var html = '';
    for (var i = 0; i < items.length; i++) {
      var e = items[i];
      var d = new Date(e.date);
      var dateStr = d.toLocaleDateString('en-US', {year:'numeric',month:'short',day:'numeric'});
      var timeStr = d.toLocaleTimeString('en-US', {hour:'2-digit',minute:'2-digit'});
      var snippetHtml = '';
      if (e.snippet && query) {
        snippetHtml = '<div class="email-snippet">' + highlight(e.snippet, query) + '</div>';
      }
      var href = '#/email/' + encodeURIComponent(e.path);
      html += '<a class="email-card" href="' + href + '">' +
        '<div class="email-subject">' + highlight(e.subject || '(no subject)', query) + '</div>' +
        snippetHtml +
        '<div class="email-meta">' +
        '<span>From: ' + esc(e.from) + '</span>' +
        '<span>To: ' + esc(e.to) + '</span>' +
        '<span>' + dateStr + ' ' + timeStr + '</span>' +
        '</div></a>';
    }
    resultsDiv.innerHTML = html;
  }

  function renderPager(data) {
    var total = (typeof data.total === 'number') ? data.total : 0;
    var limit = (typeof data.limit === 'number') ? data.limit : PAGE_SIZE;
    var offset = (typeof data.offset === 'number') ? data.offset : 0;
    var totalPages = Math.ceil(total / limit);
    var currentPage = Math.floor(offset / limit);

    if (totalPages <= 1) { pagerDiv.innerHTML = ''; return; }

    var html = '';
    // Prev button.
    html += '<button' + (currentPage === 0 ? ' disabled' : '') + ' data-page="' + (currentPage - 1) + '">&lsaquo; Prev</button>';

    // Page numbers — show a window around current page.
    var windowSize = 2;
    var startPage = Math.max(0, currentPage - windowSize);
    var endPage = Math.min(totalPages - 1, currentPage + windowSize);

    if (startPage > 0) {
      html += '<button data-page="0">1</button>';
      if (startPage > 1) html += '<span class="pager-info">&hellip;</span>';
    }

    for (var p = startPage; p <= endPage; p++) {
      html += '<button data-page="' + p + '"' + (p === currentPage ? ' class="active"' : '') + '>' + (p + 1) + '</button>';
    }

    if (endPage < totalPages - 1) {
      if (endPage < totalPages - 2) html += '<span class="pager-info">&hellip;</span>';
      html += '<button data-page="' + (totalPages - 1) + '">' + totalPages + '</button>';
    }

    // Next button.
    html += '<button' + (currentPage >= totalPages - 1 ? ' disabled' : '') + ' data-page="' + (currentPage + 1) + '">Next &rsaquo;</button>';

    pagerDiv.innerHTML = html;

    // Attach click handlers.
    pagerDiv.querySelectorAll('button[data-page]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var page = parseInt(btn.getAttribute('data-page'), 10);
        if (!isNaN(page)) goToPage(page);
      });
    });
  }

  // --- Email detail ---
  async function loadEmail(emailPath) {
    detailContent.innerHTML = '<div class="spinner-lg"></div>';
    document.title = 'Loading...';
    try {
      var r = await fetch('/api/email?path=' + encodeURIComponent(emailPath));
      if (!r.ok) throw new Error('not found');
      var e = await r.json();
      renderDetail(e);
    } catch(err) {
      detailContent.innerHTML = '<div class="empty"><p>Could not load email.</p></div>';
    }
  }

  function renderDetail(e) {
    document.title = e.subject || '(no subject)';
    var d = new Date(e.date);
    var dateStr = d.toLocaleDateString('en-US', {weekday:'long',year:'numeric',month:'long',day:'numeric'}) +
      ' at ' + d.toLocaleTimeString('en-US', {hour:'2-digit',minute:'2-digit'});

    var fields = '<dt>From</dt><dd>' + esc(e.from) + '</dd>' +
      '<dt>To</dt><dd>' + esc(e.to) + '</dd>';
    if (e.cc) fields += '<dt>CC</dt><dd>' + esc(e.cc) + '</dd>';
    if (e.reply_to) fields += '<dt>Reply-To</dt><dd>' + esc(e.reply_to) + '</dd>';
    fields += '<dt>Date</dt><dd>' + esc(dateStr) + '</dd>';
    fields += '<dt>Path</dt><dd style="font-size:0.8rem;color:var(--text-dim)">' + esc(e.path) + '</dd>';

    var hasHTML = e.html_body && e.html_body.length > 0;
    var hasText = e.text_body && e.text_body.length > 0;

    var tabs = '';
    var body = '';
    if (hasHTML && hasText) {
      tabs = '<div class="tab-bar">' +
        '<button class="tab-btn active" data-tab="html">HTML</button>' +
        '<button class="tab-btn" data-tab="text">Plain Text</button>' +
        '</div>';
      body = '<div class="tab-panel" data-panel="html"><div class="detail-body"><iframe id="email-iframe" sandbox="allow-same-origin"></iframe></div></div>' +
        '<div class="tab-panel" data-panel="text" style="display:none"><div class="detail-body-text"></div></div>';
    } else if (hasHTML) {
      body = '<div class="detail-body"><iframe id="email-iframe" sandbox="allow-same-origin"></iframe></div>';
    } else {
      body = '<div class="detail-body-text"></div>';
    }

    var attachments = '';
    if (e.attachments && e.attachments.length > 0) {
      var attItems = '';
      for (var i = 0; i < e.attachments.length; i++) {
        var a = e.attachments[i];
        attItems += '<li class="att-item">' +
          '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="m18.375 12.739-7.693 7.693a4.5 4.5 0 0 1-6.364-6.364l10.94-10.94A3 3 0 1 1 19.5 7.372L8.552 18.32m.009-.01-.01.01m5.699-9.941-7.81 7.81a1.5 1.5 0 0 0 2.112 2.13"/></svg>' +
          '<span>' + esc(a.filename || 'unnamed') + '</span>' +
          '<span class="att-size">' + formatSize(a.size) + '</span>' +
          '</li>';
      }
      attachments = '<div class="detail-attachments"><h3>Attachments (' + e.attachments.length + ')</h3><ul class="att-list">' + attItems + '</ul></div>';
    }

    detailContent.innerHTML =
      '<div class="detail-card">' +
      '<div class="detail-header">' +
      '<div class="detail-subject">' + esc(e.subject || '(no subject)') + '</div>' +
      '<dl class="detail-fields">' + fields + '</dl>' +
      '</div>' +
      tabs + body + attachments +
      '</div>';

    // Set iframe content.
    var iframe = document.getElementById('email-iframe');
    if (iframe && hasHTML) {
      iframe.srcdoc = e.html_body;
      iframe.addEventListener('load', function() {
        try {
          var h = iframe.contentDocument.documentElement.scrollHeight;
          iframe.style.height = Math.max(h + 20, 200) + 'px';
        } catch(ex) {}
      });
    }

    // Set plain text content.
    var textDiv = detailContent.querySelector('.detail-body-text');
    if (textDiv && hasText) {
      textDiv.textContent = e.text_body;
    }

    // Tab switching.
    var tabBtns = detailContent.querySelectorAll('.tab-btn');
    tabBtns.forEach(function(btn) {
      btn.addEventListener('click', function() {
        tabBtns.forEach(function(b) { b.classList.remove('active'); });
        btn.classList.add('active');
        var tab = btn.getAttribute('data-tab');
        detailContent.querySelectorAll('.tab-panel').forEach(function(p) {
          p.style.display = p.getAttribute('data-panel') === tab ? '' : 'none';
        });
        // Resize iframe when switching to HTML tab.
        if (tab === 'html') {
          var ifr = document.getElementById('email-iframe');
          if (ifr) {
            try {
              var h = ifr.contentDocument.documentElement.scrollHeight;
              ifr.style.height = Math.max(h + 20, 200) + 'px';
            } catch(ex) {}
          }
        }
      });
    });
  }

  // --- Helpers ---
  function highlight(text, query) {
    if (!query) return esc(text);
    var re = new RegExp('(' + query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'gi');
    return esc(text).replace(re, '<mark>$1</mark>');
  }

  function esc(s) {
    if (!s) return '';
    var d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1048576).toFixed(1) + ' MB';
  }

  // --- Sync ---
  var syncBtn = document.getElementById('sync-btn');
  var syncDropdown = document.getElementById('sync-dropdown');
  var syncAllBtn = document.getElementById('sync-all-btn');
  var syncAccountsDiv = document.getElementById('sync-accounts');
  var syncFooter = document.getElementById('sync-footer');
  var syncPollTimer = null;
  var syncAvailable = true;

  syncBtn.addEventListener('click', function(e) {
    e.stopPropagation();
    var isOpen = syncDropdown.classList.contains('open');
    syncDropdown.classList.toggle('open');
    if (!isOpen) loadAccounts();
  });

  // Close dropdown when clicking outside.
  document.addEventListener('click', function(e) {
    if (!syncDropdown.contains(e.target) && e.target !== syncBtn) {
      syncDropdown.classList.remove('open');
    }
  });

  syncAllBtn.addEventListener('click', function() {
    triggerSync(null);
  });

  async function loadAccounts() {
    try {
      var r = await fetch('/api/accounts');
      if (!r.ok) throw new Error('unavailable');
      var accounts = await r.json();
      syncAvailable = true;
      renderSyncAccounts(accounts);
    } catch(e) {
      syncAvailable = false;
      syncAccountsDiv.innerHTML = '<div style="padding:1rem;text-align:center;color:var(--text-dim);font-size:0.82rem">Sync service unavailable.<br>Start the <code>mail-sync</code> container.</div>';
      syncAllBtn.disabled = true;
    }
  }

  function renderSyncAccounts(accounts) {
    var anySyncing = false;
    var html = '';
    for (var i = 0; i < accounts.length; i++) {
      var a = accounts[i];
      var statusClass = '';
      var statusText = '';
      if (a.syncing) {
        statusClass = 'syncing';
        statusText = '<span class="spinner" style="width:12px;height:12px;border-width:1.5px;vertical-align:middle"></span> Syncing...';
        anySyncing = true;
      } else if (a.last_error) {
        statusClass = 'error';
        statusText = 'Error';
      } else if (a.last_sync) {
        statusClass = 'done';
        var ago = timeSince(a.last_sync);
        statusText = a.new_messages ? ('+' + a.new_messages + ' new, ' + ago) : ago;
      }
      html += '<div class="sync-account">' +
        '<div class="sync-account-info">' +
        '<div class="sync-account-name">' + esc(a.name) + '</div>' +
        '<span class="sync-account-type">' + esc(a.type) + '</span>' +
        '</div>' +
        (statusText ? '<span class="sync-account-status ' + statusClass + '">' + statusText + '</span>' : '') +
        '<button class="sync-account-btn" data-account="' + esc(a.name) + '"' + (a.syncing ? ' disabled' : '') + '>Sync</button>' +
        '</div>';
    }
    syncAccountsDiv.innerHTML = html;
    syncAllBtn.disabled = anySyncing;

    // Attach per-account sync buttons.
    syncAccountsDiv.querySelectorAll('.sync-account-btn').forEach(function(btn) {
      btn.addEventListener('click', function() {
        triggerSync(btn.getAttribute('data-account'));
      });
    });

    // Keep polling if any syncing.
    if (anySyncing && !syncPollTimer) {
      syncPollTimer = setInterval(pollSyncStatus, 2000);
    }
    if (!anySyncing && syncPollTimer) {
      clearInterval(syncPollTimer);
      syncPollTimer = null;
    }
  }

  async function triggerSync(accountName) {
    try {
      var body = accountName ? { account: accountName } : {};
      var r = await fetch('/api/sync', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
      });
      var data = await r.json();
      if (r.status === 409) {
        // Already syncing.
      }
      // Start polling.
      if (!syncPollTimer) {
        syncPollTimer = setInterval(pollSyncStatus, 2000);
      }
      await loadAccounts();
    } catch(e) {}
  }

  async function pollSyncStatus() {
    try {
      var r = await fetch('/api/accounts');
      if (!r.ok) return;
      var accounts = await r.json();
      renderSyncAccounts(accounts);

      var anySyncing = accounts.some(function(a) { return a.syncing; });
      if (!anySyncing) {
        // Sync finished — auto-reindex + refresh.
        clearInterval(syncPollTimer);
        syncPollTimer = null;
        syncFooter.textContent = 'Reindexing...';
        syncFooter.classList.add('visible');
        await runReindexWithProgress();
        syncFooter.textContent = 'Done! Index refreshed.';
        setTimeout(function() { syncFooter.classList.remove('visible'); }, 3000);
      }
    } catch(e) {}
  }

  async function runReindexWithProgress() {
    var r = await fetch('/api/reindex', {method:'POST'});
    if (r.status === 409) {
      // Already running, just poll to show progress
    } else if (r.status !== 202) {
      reindexProgress.style.display = 'none';
      return;
    }
    reindexProgress.style.display = 'block';
    reindexProgressText.textContent = 'Starting...';
    reindexProgressFill.style.width = '0%';

    var poll = setInterval(async function() {
      try {
        var s = await fetch('/api/reindex/status');
        var d = await s.json();
        if (d.phase === 'parquet') {
          reindexProgressText.textContent = 'Building keyword index...';
          reindexProgressFill.style.width = '5%';
        } else if (d.phase === 'vector' && d.vec_total > 0) {
          var pct = Math.round((d.vec_indexed / d.vec_total) * 95) + 5;
          reindexProgressText.textContent = 'Indexing vectors: ' + d.vec_indexed.toLocaleString() + ' / ' + d.vec_total.toLocaleString();
          reindexProgressFill.style.width = pct + '%';
        } else if (d.phase === 'vector') {
          reindexProgressText.textContent = 'Preparing vector index...';
          reindexProgressFill.style.width = '5%';
        }
        if (!d.running) {
          clearInterval(poll);
          reindexProgressFill.style.width = '100%';
          if (d.error) {
            reindexProgressText.textContent = 'Error: ' + d.error;
          } else {
            var totalMsg = (d.indexed || 0).toLocaleString() + ' emails';
          if (d.vec_indexed > 0) totalMsg += ' (' + d.vec_indexed.toLocaleString() + ' vectors)';
          reindexProgressText.textContent = 'Done! Indexed ' + totalMsg;
          }
          await loadStats();
          currentOffset = 0;
          await doSearch(qInput.value, 0);
          setTimeout(function() {
            reindexProgress.style.display = 'none';
          }, 3000);
        }
      } catch(e) {}
    }, 1500);
  }

  function timeSince(epochSeconds) {
    var seconds = Math.floor(Date.now() / 1000 - epochSeconds);
    if (seconds < 60) return seconds + 's ago';
    var minutes = Math.floor(seconds / 60);
    if (minutes < 60) return minutes + 'm ago';
    var hours = Math.floor(minutes / 60);
    if (hours < 24) return hours + 'h ago';
    return Math.floor(hours / 24) + 'd ago';
  }

  // --- Events ---
  qInput.addEventListener('input', function() {
    clearTimeout(debounceTimer);
    currentOffset = 0;
    debounceTimer = setTimeout(function() { doSearch(qInput.value, 0); }, 200);
  });

  // Mode toggle
  var modeKeyword = document.getElementById('mode-keyword');
  var modeSimilarity = document.getElementById('mode-similarity');
  if (modeKeyword) modeKeyword.addEventListener('click', function() {
    searchMode = 'keyword';
    modeKeyword.classList.add('active');
    if (modeSimilarity) modeSimilarity.classList.remove('active');
    currentOffset = 0;
    doSearch(qInput.value, 0);
  });
  if (modeSimilarity) modeSimilarity.addEventListener('click', function() {
    searchMode = 'similarity';
    modeSimilarity.classList.add('active');
    if (modeKeyword) modeKeyword.classList.remove('active');
    currentOffset = 0;
    doSearch(qInput.value, 0);
  });
  if (modeKeyword) modeKeyword.classList.add('active');

  reindexBtn.addEventListener('click', async function() {
    reindexBtn.disabled = true;
    reindexBtn.innerHTML = '<span class="spinner"></span>';
    try {
      await runReindexWithProgress();
    } finally {
      reindexBtn.disabled = false;
      reindexBtn.textContent = 'Reindex';
    }
  });

  // --- Init ---
  loadStats();
  doSearch('', 0);
  route();
})();
</script>
</body>
</html>`
