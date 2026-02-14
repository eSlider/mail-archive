# Mail Archive

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/eSlider/mail-archive/actions/workflows/ci.yml/badge.svg)](https://github.com/eSlider/mail-archive/actions/workflows/ci.yml)
[![Docker](https://github.com/eSlider/mail-archive/actions/workflows/docker.yml/badge.svg)](https://github.com/eSlider/mail-archive/actions/workflows/docker.yml)

Multi-user email archival system with search. Syncs emails from IMAP, POP3, and Gmail API accounts into a structured filesystem. **Never deletes or marks emails as read.**

## Features

- **Multi-user** — username/password registration (no email verification), optional OAuth2 (GitHub, Google, Facebook)
- **Multi-account** — each user manages their own email accounts
- **Protocol support** — IMAP, POP3, Gmail API
- **PST/OST import** — upload Outlook archive files (10GB+), streamed with progress
- **Deduplication** — SHA-256 content checksums prevent duplicate storage
- **Search** — keyword search (DuckDB + Parquet) and similarity search (Qdrant + Ollama)
- **Live sync** — cancel running syncs, real-time progress, auto-reindex every 5s
- **Date preservation** — file mtime set from email Date/Received headers
- **UUIDv7 IDs** — time-ordered identifiers for all entities
- **Raw storage** — emails preserved as `.eml` files (RFC 822), readable by any mail client
- **Per-user isolation** — all data under `users/{uuid}/`

## Quick Start

```bash
# 1. Clone and build
git clone https://github.com/eSlider/mails.git
cd mails
go build ./cmd/mails

# 2. Run
./mails serve

# 3. Open browser and register
open http://localhost:8090
```

For Docker:

```bash
cp .env.example .env   # optional: configure OAuth providers
docker compose up -d
open http://localhost:8090
```

Docker images are published to `ghcr.io/eSlider/mail-archive` with SemVer tags (e.g. `v1.0.1`, `1.0.1`) and `latest`. Use a specific version for production:

```bash
docker pull ghcr.io/eslider/mail-archive:v1.0.1
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8090` | HTTP listen address |
| `DATA_DIR` | `./users` | Base directory for user data |
| `BASE_URL` | `http://localhost:8090` | Public URL for OAuth callbacks |
| `GITHUB_CLIENT_ID` | — | GitHub OAuth app client ID |
| `GITHUB_CLIENT_SECRET` | — | GitHub OAuth app client secret |
| `GOOGLE_CLIENT_ID` | — | Google OAuth app client ID |
| `GOOGLE_CLIENT_SECRET` | — | Google OAuth app client secret |
| `FACEBOOK_CLIENT_ID` | — | Facebook OAuth app client ID |
| `FACEBOOK_CLIENT_SECRET` | — | Facebook OAuth app client secret |
| `QDRANT_URL` | — | Qdrant gRPC address for similarity search |
| `OLLAMA_URL` | — | Ollama API URL for embeddings |
| `EMBED_MODEL` | `all-minilm` | Embedding model name |

### OAuth Setup (Optional)

OAuth is not required. Users can register with email + password by default.

To enable OAuth login, configure one or more providers:

1. **GitHub:** Create an OAuth App at [github.com/settings/applications/new](https://github.com/settings/applications/new). Set callback URL to `{BASE_URL}/auth/github/callback`.

2. **Google:** Create credentials at [console.cloud.google.com](https://console.cloud.google.com/apis/credentials). Set redirect URI to `{BASE_URL}/auth/google/callback`.

3. **Facebook:** Create an app at [developers.facebook.com](https://developers.facebook.com/apps/). Set redirect URI to `{BASE_URL}/auth/facebook/callback`.

## User Data Layout

```
users/
  019c56a4-a9ef-79bd-b53a-ef7a080d9c90/
    user.json                    # User metadata
    accounts.yml                 # Email account configs
    sync.sqlite                  # Sync state database
    logs/                        # Structured sync logs
    gmail.com/
      eslider/
        inbox/
          a1b2c3d4e5f67890-12345.eml
        gmail/
          sent/
            b2c3d4e5f6789012-12346.eml
        index.parquet            # Search index
```

## Architecture

```
cmd/
  mails/           → Entry point, CLI (serve, fix-dates, version)
  mail-search/     → Standalone search CLI (optional, EMAILS_DIR + Qdrant/Ollama)
internal/
  auth/            → OAuth2 (GitHub, Google, Facebook), sessions
  user/            → User storage (users/{uuid}/)
  account/         → Email account CRUD (accounts.yml)
  model/           → Shared types (User, Account, SyncJob)
  sync/            → Sync orchestration, live indexing, cancel support
    imap/          → IMAP protocol sync (UID-based, context-aware)
    pop3/          → POP3 protocol sync
    gmail/         → Gmail API sync
    pst/           → PST/OST file import (go-pst; readpst fallback for newer OST)
  search/
    eml/           → .eml parser (charset, MIME, fuzzy date parsing)
    index/         → DuckDB search index → Parquet (with cache cleanup)
    vector/        → Qdrant similarity search
  web/             → Chi router, HTTP handlers
web/
  static/          → CSS, JS, Vue templates (all local, no CDN, no build step)
```

## Development

```bash
# Build
go build ./cmd/mails

# Run locally
DATA_DIR=./users ./mails serve

# Fix file timestamps on all .eml files
./mails fix-dates

# Run unit tests
go test ./...

# Run e2e tests (requires GreenMail + Qdrant + Ollama)
docker compose --profile test up -d greenmail
go test -tags e2e -v ./tests/e2e/

# Docker dev mode (auto-rebuild on changes)
docker compose watch
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.24+ |
| Frontend | jQuery 3.7, Vue.js 3.5 (no build step, no CDN) |
| Search index | DuckDB (in-memory) + Parquet (persistence) |
| Vector search | Qdrant + Ollama embeddings |
| Sync state | SQLite (per-user) |
| Auth | bcrypt passwords, optional OAuth2 |
| Container | Docker + Docker Compose |
| License | MIT |

## Frontend

The frontend uses **Vue.js 3** and **jQuery** with zero build tooling -- no webpack, no Vite, no TypeScript. All vendor libraries are committed locally under `web/static/js/vendor/` (no CDN dependency).

### Template Loading

Vue templates are stored as standalone `.vue` files containing raw HTML with Vue directives. At startup, `main.js` fetches the template asynchronously before creating the Vue app:

```javascript
// main.js — async bootstrap
(async function () {
  var res = await fetch('/static/js/app/main.template.vue');
  var template = await res.text();

  var App = {
    template: template,
    data: function () { return { /* ... */ } },
    methods: { /* ... */ }
  };

  Vue.createApp(App).mount('#app');
})();
```

This keeps the template editable as a proper `.vue` file (with IDE syntax highlighting and linting) while avoiding any compile/transpile step. The pattern is inspired by the [Produktor UI](https://produktor.github.io/ui/) approach to dynamic component loading.

### File Structure

```
web/static/
  css/app.css                        # Application styles (dark theme, CSS vars)
  js/
    vendor/
      jquery-3.7.1.min.js            # jQuery (local copy)
      vue-3.5.13.global.prod.js      # Vue.js (local copy)
    app/
      main.js                        # App logic (data, methods, computed)
      main.template.vue              # Vue template (HTML with directives)
```

## API

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full API reference.

```bash
# Health check (no auth)
curl http://localhost:8090/health

# Search (requires session cookie)
curl -b cookies.txt "http://localhost:8090/api/search?q=invoice&limit=20"

# List accounts
curl -b cookies.txt http://localhost:8090/api/accounts

# Trigger sync
curl -b cookies.txt -X POST http://localhost:8090/api/sync

# Stop a running sync
curl -b cookies.txt -X POST http://localhost:8090/api/sync/stop   -H 'Content-Type: application/json' -d '{"account_id":"..."}'

# Import PST/OST file
curl -b cookies.txt -X POST http://localhost:8090/api/import/pst   -F "file=@archive.pst" -F "title=My Outlook Archive"

# For newer Outlook OST files, install pst-utils as fallback: apt install pst-utils

# Check import progress
curl -b cookies.txt http://localhost:8090/api/import/status/{job_id}
```

## License

MIT — see [LICENSE](LICENSE).
