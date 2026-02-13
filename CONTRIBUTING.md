# Contributing

Guidelines for contributing to the Mail Archive project. Inspired by [Gitea's backend](https://docs.gitea.com/contributing/guidelines-backend) and [frontend](https://docs.gitea.com/contributing/guidelines-frontend) guidelines.

## Architecture

```
mails/
├── cmd/mails/           # Application entry point
├── internal/            # Private application packages
│   ├── auth/            # OAuth2 login (GitHub, Google, Facebook)
│   ├── user/            # User management, UUIDv7 IDs
│   ├── account/         # Per-user email account CRUD
│   ├── model/           # Shared data types
│   ├── sync/            # Email sync orchestration
│   │   ├── imap/        # IMAP protocol sync
│   │   ├── pop3/        # POP3 protocol sync
│   │   └── gmail/       # Gmail API sync
│   ├── search/          # Email search & indexing
│   │   ├── eml/         # .eml file parser
│   │   ├── index/       # DuckDB + Parquet index
│   │   └── vector/      # Qdrant similarity search
│   └── web/             # HTTP router, handlers, middleware
├── web/                 # Frontend assets
│   ├── static/
│   │   ├── css/         # Application styles
│   │   └── js/
│   │       ├── vendor/  # jQuery, Vue.js (local, no CDN)
│   │       └── app/     # Application JavaScript
│   └── templates/       # Go HTML templates
├── users/               # Per-user data (gitignored)
│   └── {uuid}/
│       ├── user.json
│       ├── accounts.yml
│       ├── sync.sqlite
│       ├── logs/
│       └── {domain}/{local-part}/
│           ├── inbox/
│           ├── gmail/sent/
│           └── index.parquet
├── scripts/             # Legacy Python scripts (reference only)
├── go.mod
├── docker-compose.yml
└── Dockerfile
```

## Package Dependencies

Packages follow a strict dependency hierarchy to avoid import cycles:

```
cmd → internal/web → internal/sync → internal/model
                   → internal/auth
                   → internal/user
                   → internal/account
                   → internal/search
```

- Left packages may depend on right packages
- Right packages **MUST NOT** depend on left packages
- Sub-packages at the same level use interfaces to avoid circular imports

### Import Aliases

When multiple packages share similar names, use **snake_case** import aliases:

```go
import (
    sync_imap "github.com/eslider/mails/internal/sync/imap"
    sync_pop3 "github.com/eslider/mails/internal/sync/pop3"
)
```

### Package Names

- Top-level packages: plural (e.g. `internal`)
- Sub-packages: singular (e.g. `internal/user`, `internal/account`)

## Backend Guidelines

### Language & Style

- **Go 1.25+**, strict typing
- **No classes unless necessary** — prefer pure functions, minimal structs
- **Minimal dependencies** — stdlib first, then well-maintained packages
- **Comments in English only**
- All IDs use **UUIDv7** (time-ordered) via `model.NewID()`

### Key Rules

1. **NEVER delete or mark emails as read** on the remote server. Sync is read-only.
2. **NEVER expose passwords** via JSON API responses. Use `json:"-"` tag.
3. **Test first** — write the test, then implement.
4. **Log everything** at INFO level, errors with full tracebacks.
5. When adding database migrations, include both up and down paths.
6. Use `context.Context` as first parameter for functions that do I/O.

### Error Handling

- Return errors, don't panic
- Wrap errors with context: `fmt.Errorf("sync account %s: %w", id, err)`
- Log warnings for non-fatal errors, continue processing

### Database

- **SQLite** for per-user sync state (`sync.sqlite`)
- **DuckDB** for search index (in-memory, persisted as Parquet)
- Use WAL mode for SQLite: `?_journal_mode=WAL`
- Use transactions for multi-row operations

### Testing

```bash
# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run specific package tests
go test ./internal/search/eml/
```

Use `testing.T` and table-driven tests. Mock external services (IMAP, POP3, APIs).

## Frontend Guidelines

### Stack

- **jQuery 3.7** for DOM manipulation and AJAX
- **Vue.js 3.5** for reactive UI components
- **No TypeScript** — plain JavaScript (ECMAScript)
- **No CDN** — all resources served locally from `web/static/js/vendor/`

### Style Guide

Based on [Google JavaScript Style Guide](https://google.github.io/styleguide/jsguide.html):

1. Use `var` for compatibility (no `let`/`const` in hot paths if IE11 needed)
2. HTML IDs and classes use **kebab-case** with 2-3 feature keywords
3. JavaScript-only classes use `js-` prefix
4. No inline `<style>` or `<script>` — use external files
5. Use semantic HTML elements (`<button>` not `<div>`)

### Framework Usage

- **Vue 3** for reactive components (search, accounts, sync status)
- **jQuery** for AJAX calls (`$.getJSON`, `$.ajax`) and simple DOM operations
- Do **NOT** mix Vue and jQuery on the same DOM elements
- Use Vue's `$nextTick` for post-render DOM access

### CSS Guidelines

1. Use CSS custom properties (variables) for theming
2. Dark theme by default (see `:root` variables in `app.css`)
3. Avoid `!important` — add comments if unavoidable
4. Mobile-first responsive design with `@media` breakpoints
5. BEM-like naming: `.email-card`, `.email-subject`, `.email-meta-row`

### Accessibility

- All interactive elements must be keyboard-accessible
- Use proper ARIA labels on icon-only buttons
- Ensure sufficient color contrast (WCAG AA)
- Form inputs must have associated `<label>` elements

## User Data Layout

Each user's data lives under `users/{uuidv7}/`:

| Path | Purpose |
|------|---------|
| `user.json` | User metadata (name, email, provider) |
| `accounts.yml` | Email account configurations |
| `sync.sqlite` | Sync jobs, UIDs, state |
| `logs/{job-id}.jsonl` | Structured sync logs |
| `{domain}/{local}/` | Downloaded .eml files |
| `{domain}/{local}/index.parquet` | Search index per account |

### Email Storage

Emails are stored as raw `.eml` files preserving RFC 822 format:

```
users/{uuid}/gmail.com/eslider/inbox/a1b2c3d4e5f67890-12345.eml
```

- Filename: `{sha256-prefix-16}-{uid}.eml`
- File mtime: set to email's Date header
- Deduplication: by content checksum (IMAP/POP3) or message ID (Gmail)

## Docker

```bash
# Development
docker compose up

# Production build
docker compose -f docker-compose.yml up -d

# Run tests
docker compose run --rm mails go test ./...
```

## API Reference

All API endpoints require authentication (session cookie or `Authorization: Bearer <token>`).

### Auth

| Method | Path | Description |
|--------|------|-------------|
| GET | `/login` | Login page |
| GET | `/auth/{provider}` | Start OAuth flow |
| GET | `/auth/{provider}/callback` | OAuth callback |
| POST | `/logout` | End session |

### User

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/me` | Current user info |

### Accounts

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/accounts` | List email accounts |
| POST | `/api/accounts` | Add new account |
| PUT | `/api/accounts/{id}` | Update account |
| DELETE | `/api/accounts/{id}` | Remove account |

### Sync

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/sync` | Trigger sync (all or specific account) |
| GET | `/api/sync/status` | Sync status per account |

### Search

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/search?q=&limit=&offset=&mode=` | Search emails |
| GET | `/api/email?path=` | Get single email detail |
| GET | `/api/stats` | Index statistics |
| POST | `/api/reindex` | Rebuild search index |

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check (no auth required) |

## Common Issues

- **OAuth redirect mismatch:** Ensure `BASE_URL` matches the registered redirect URI in your OAuth app settings.
- **IMAP connection refused:** Check host, port, and SSL settings. Gmail requires an App Password (not regular password).
- **Search returns 0 results:** Run reindex after syncing new emails.
- **SQLite busy:** Increase `_busy_timeout` or reduce concurrent sync jobs.
