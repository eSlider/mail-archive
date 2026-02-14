# AGENTS.md — Instructions for AI Agents Working on This Project

> **Contributors:** See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed guidelines, API reference, and coding standards.

## Project Context

This is a **multi-user email archival system** that syncs emails from multiple accounts (IMAP, POP3, Gmail API) per user into a hierarchical structure: `users/{uuid}/{domain}/{local}/{folder}/{checksum}-{id}.eml`. Users authenticate via username/password or OAuth2 (GitHub, Google, Facebook). All IDs are UUIDv7.

## Architecture

```
mails/
├── cmd/mails/           # Application entry point
├── internal/
│   ├── auth/            # OAuth2 + bcrypt, session management
│   ├── user/            # User CRUD (users/{uuid}/)
│   ├── account/         # Per-user email account configs
│   ├── model/           # Shared types (User, Account, SyncJob)
│   ├── sync/            # Sync orchestration
│   │   ├── imap/        # IMAP protocol
│   │   ├── pop3/        # POP3 protocol
│   │   ├── gmail/       # Gmail API
│   │   └── pst/         # PST/OST import
│   ├── search/
│   │   ├── eml/         # .eml parser
│   │   ├── index/       # DuckDB + Parquet
│   │   └── vector/      # Qdrant + Ollama
│   └── web/             # Chi HTTP router + handlers
├── web/static/          # CSS, JS (Vue.js 3, native fetch)
├── internal/web/templates/  # Go HTML templates
├── users/               # Per-user data (gitignored)
├── scripts/             # Legacy Python scripts (reference)
├── docker-compose.yml
├── Dockerfile
└── go.mod
```

## Key Design Decisions

- **Multi-user** — each user has isolated data under `users/{uuid}/`
- **Auth** — username/password (bcrypt) or OAuth2 (GitHub, Google, Facebook)
- **UUIDv7 IDs** — time-ordered for all entities
- **SHA-256 dedup** — content checksums prevent duplicate emails
- **SQLite per user** — sync state in `sync.sqlite`
- **Raw .eml storage** — preserves RFC 822 format
- **No CDN** — Vue.js served locally, native fetch for HTTP
- **Go backend** — single binary, Chi router
- **NEVER delete emails** from server — sync is read-only

## Package Dependencies

```
cmd → internal/web → internal/sync → internal/model
                   → internal/auth
                   → internal/user
                   → internal/account
                   → internal/search
```

## Coding Guidelines

- **Do NOT add yourself as co-author**
- **NEVER REMOVE EMAILS** from server
- **Go 1.25+**, strict typing
- **No classes unless necessary** — prefer pure functions
- **Minimal dependencies** — stdlib first
- **Comments in English only**
- **Test first** — write the test, then implement
- All user data in `users/{uuid}/`
- Keep sync modules independent — each handles one protocol
- Log everything at INFO level, errors with full tracebacks
- Frontend: Vue.js 3, plain JavaScript (no TypeScript), native fetch for API calls
- Use snake_case import aliases for disambiguation

## Next Steps (Priority Order)

### 1. Tests (High Priority)

Write unit tests using `go test`. Key areas:

- `internal/auth/`: session create/get/delete, token generation
- `internal/user/`: FindOrCreate, directory creation
- `internal/account/`: CRUD operations, YAML serialization
- `internal/sync/state.go`: SQLite state tracking
- `internal/search/eml/`: parser tests
- `internal/search/index/`: index tests
- Integration: GreenMail for end-to-end IMAP/POP3 testing

### 2. Gmail API Sync (Medium Priority)

Complete the Gmail API sync implementation in `internal/sync/gmail/`:

- OAuth2 token management using `golang.org/x/oauth2/google`
- Gmail API client for message listing and raw RFC822 fetch
- Label → filesystem path mapping
- Device code flow for headless Docker environments

### 3. Search Per User/Account (Medium Priority)

Refactor search to work per-user:

- Index emails per account into `users/{uuid}/{domain}/{local}/index.parquet`
- Cross-account search within a single user
- Cache indices in memory with LRU eviction

### 4. Scheduled Sync (Medium Priority)

Implement periodic sync based on account `sync.interval`:

- Background goroutine per user with ticker
- Structured JSONL logs in `users/{uuid}/logs/`
- Health endpoint with per-account sync status

### 5. Data Migration (Low Priority)

Migrate existing `emails/` data to new `users/{uuid}/` structure:

- Script to create user, import accounts from `config/*.yml`
- Move `.eml` files to new path format
- Convert `.sync_state` files to SQLite entries
