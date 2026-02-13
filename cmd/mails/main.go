// mail-archive is a multi-user email archival system with search.
//
// Usage:
//
//	mails serve    Start the HTTP server
//	mails version  Print version information
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

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
  serve     Start the HTTP server
  version   Print version information

Environment:
  LISTEN_ADDR         HTTP listen address (default: :8080)
  DATA_DIR            Base data directory (default: ./users)
  BASE_URL            Public base URL for OAuth callbacks (default: http://localhost:8080)

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
	listenAddr := envOr("LISTEN_ADDR", ":8080")
	dataDir := envOr("DATA_DIR", "./users")
	baseURL := envOr("BASE_URL", "http://localhost:8080")

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

	// Set static assets path.
	staticDir := envOr("STATIC_DIR", "./web/static")
	web.StaticDir = staticDir

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
