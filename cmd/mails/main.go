// mail-archive is a multi-user email archival system with search.
//
// Usage:
//
//	mails serve    Start the HTTP server
//	mails version  Print version information
package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eslider/mails/internal/account"
	"github.com/eslider/mails/internal/auth"
	"github.com/eslider/mails/internal/sync"
	"github.com/eslider/mails/internal/user"
	"github.com/eslider/mails/internal/web"
)

var version = "1.0.0-dev"

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "fix-dates":
		runFixDates()
	case "version":
		fmt.Printf("mails %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: mails <command>

Commands:
  serve       Start the HTTP server
  fix-dates   Fix mtime on all .eml files using Date/Received headers
  version     Print version information

Environment:
  LISTEN_ADDR         HTTP listen address (default: :8090)
  DATA_DIR            Base data directory (default: ./users)
  BASE_URL            Public base URL for OAuth callbacks (default: http://localhost:8090)

  GITHUB_CLIENT_ID    GitHub OAuth app client ID
  GITHUB_CLIENT_SECRET GitHub OAuth app client secret
  GOOGLE_CLIENT_ID    Google OAuth app client ID
  GOOGLE_CLIENT_SECRET Google OAuth app client secret
  FACEBOOK_CLIENT_ID  Facebook OAuth app client ID
  FACEBOOK_CLIENT_SECRET Facebook OAuth app client secret

  QDRANT_URL          Qdrant gRPC address for similarity search
  OLLAMA_URL          Ollama API URL for embeddings
  EMBED_MODEL         Ollama embedding model (default: all-minilm)`)
}

func runServe() {
	listenAddr := envOr("LISTEN_ADDR", ":8090")
	dataDir := envOr("DATA_DIR", "./users")
	baseURL := envOr("BASE_URL", "http://localhost:8090")

	// Initialize stores.
	userStore, err := user.NewStore(dataDir)
	if err != nil {
		log.Fatalf("Failed to init user store: %v", err)
	}

	sessionStore, err := auth.NewSessionStore(dataDir)
	if err != nil {
		log.Fatalf("Failed to init session store: %v", err)
	}

	accountStore := account.NewStore(dataDir)
	syncService := sync.NewService(dataDir, accountStore)

	// Configure OAuth providers.
	var ghCfg, glCfg, fbCfg *auth.ProviderConfig

	if id := os.Getenv("GITHUB_CLIENT_ID"); id != "" {
		ghCfg = &auth.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		}
	}
	if id := os.Getenv("GOOGLE_CLIENT_ID"); id != "" {
		glCfg = &auth.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		}
	}
	if id := os.Getenv("FACEBOOK_CLIENT_ID"); id != "" {
		fbCfg = &auth.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("FACEBOOK_CLIENT_SECRET"),
		}
	}

	providers := auth.NewProviders(baseURL, ghCfg, glCfg, fbCfg)

	// Set static assets and templates path.
	staticDir := envOr("STATIC_DIR", "./web/static")
	templateDir := envOr("TEMPLATE_DIR", "")
	web.StaticDir = staticDir
	web.TemplateDir = templateDir
	if templateDir != "" {
		web.ReloadTemplates()
	}

	// Build router.
	router := web.NewRouter(web.Config{
		Users:      userStore,
		Accounts:   accountStore,
		Sessions:   sessionStore,
		Auth:       providers,
		Sync:       syncService,
		UsersDir:   dataDir,
		QdrantURL:  envOr("QDRANT_URL", ""),
		OllamaURL:  envOr("OLLAMA_URL", ""),
		EmbedModel: envOr("EMBED_MODEL", "all-minilm"),
	})

	log.Printf("Starting mail-archive %s on %s", version, listenAddr)
	log.Printf("Data directory: %s", dataDir)
	log.Printf("OAuth providers: %v", providers.Available())

	if err := http.ListenAndServe(listenAddr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func runFixDates() {
	dataDir := envOr("DATA_DIR", "./users")
	fixed := 0
	skipped := 0
	errors := 0

	err := filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".eml") {
			return nil
		}

		date := extractEmailDate(path)
		if date.IsZero() {
			skipped++
			return nil
		}

		info, err := d.Info()
		if err != nil {
			errors++
			return nil
		}

		// Only update if mtime differs by more than 1 minute.
		if info.ModTime().Sub(date).Abs() < time.Minute {
			skipped++
			return nil
		}

		if err := os.Chtimes(path, date, date); err != nil {
			log.Printf("WARN: %s: %v", path, err)
			errors++
			return nil
		}
		fixed++
		if fixed%1000 == 0 {
			log.Printf("Progress: %d fixed, %d skipped, %d errors", fixed, skipped, errors)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Walk error: %v", err)
	}
	log.Printf("Done: %d fixed, %d skipped, %d errors", fixed, skipped, errors)
}

// extractEmailDate parses the Date header from an .eml file,
// falling back to the first Received header.
func extractEmailDate(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	msg, err := mail.ReadMessage(bufio.NewReader(f))
	if err != nil {
		return time.Time{}
	}

	date, _ := msg.Header.Date()
	if !date.IsZero() {
		return date
	}

	// Fallback: try fuzzy date parsing for non-standard Date headers.
	if raw := msg.Header.Get("Date"); raw != "" {
		for _, layout := range []string{
			time.RFC1123Z,
			time.RFC1123,
			"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
			"Mon, 2 Jan 2006 15:04:05 -0700",
			"Mon, 2 Jan 2006 15:04:05",
			"2 Jan 2006 15:04:05 -0700",
			"2 Jan 2006 15:04:05",
			time.RFC822Z,
			time.RFC822,
		} {
			if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
				return t
			}
		}
	}

	// Fallback: parse first Received header.
	received := msg.Header.Get("Received")
	if received == "" {
		return time.Time{}
	}
	idx := strings.LastIndex(received, ";")
	if idx < 0 {
		return time.Time{}
	}
	dateStr := strings.TrimSpace(received[idx+1:])
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
		time.RFC822Z,
		time.RFC822,
	} {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t
		}
	}
	return time.Time{}
}
