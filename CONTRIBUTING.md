# Contributing

Guidelines for contributing to the Mail Archive project. Inspired by [Gitea's backend](https://docs.gitea.com/contributing/guidelines-backend) and [frontend](https://docs.gitea.com/contributing/guidelines-frontend) guidelines.

> **AI agents:** See [AGENTS.md](AGENTS.md) for project context and next steps.

## Architecture

```
mails/
├── cmd/mails/           # Application entry point
├── internal/            # Private application packages
│   ├── auth/            # OAuth2 login (GitHub, Google, Facebook)
│   ├── user/            # User management, UUIDv7 IDs
│   ├── account/         # Per-user email account CRUD
│   ├── model/           # Shared data types
│   ├── sync/            # Email sync orchestration, live indexing
│   │   ├── imap/        # IMAP protocol sync (UID-based, cancellable)
│   │   ├── pop3/        # POP3 protocol sync
│   │   ├── gmail/       # Gmail API sync
│   │   └── pst/         # PST/OST file import (go-pst library)
│   ├── search/
│   │   ├── eml/         # .eml file parser
│   │   ├── index/       # DuckDB + Parquet index
│   │   └── vector/      # Qdrant similarity search
│   └── web/             # HTTP router, handlers, middleware
├── web/                 # Frontend assets
│   └── static/
│       ├── css/         # Application styles
│       └── js/
│           ├── vendor/  # Vue.js 3.5 (local, no CDN)
│           └── app/     # App logic + Vue templates (.vue), native fetch
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

- **Go 1.24+**, strict typing
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

## Templates (Gitea-style)

HTML templates are stored in separate `.tmpl` files and embedded at build time:

```
internal/web/
  templates/
    auth/
      login.tmpl     # Login page ({{.Error}} for validation messages)
      register.tmpl  # Register page
    dashboard.tmpl   # SPA shell (Vue app mount point)
  templates.go      # embed + parse + renderLogin/renderRegister/renderDashboard
```

- Use `html/template` for escaping; auth templates accept `{{.Error}}`.
- Set `TEMPLATE_DIR` to override with custom files at runtime (e.g. `./internal/web/templates` for dev).
- Call `web.ReloadTemplates()` after changing `TemplateDir`.

## Frontend Guidelines

### Stack

- **Vue.js 3.5** for reactive UI components
- **Native fetch** for API calls (no jQuery)
- **No TypeScript** — plain JavaScript (ES6+)
- **No CDN** — all resources served locally from `web/static/js/vendor/`
- **No build step** — no webpack, Vite, or transpiler

### Template Loading

Vue templates are stored as standalone `.vue` files containing raw HTML with Vue directives.
The app entry point (`main.js`) fetches the template via `fetch()` before mounting:

```
web/static/js/app/
  main.js               # App logic: data, computed, methods, async bootstrap
  main.template.vue     # Vue template: pure HTML with Vue directives
```

This approach gives you:

- **IDE support** — `.vue` extension enables syntax highlighting, linting, and Emmet in editors
- **Separation of concerns** — template markup is separate from JavaScript logic
- **Zero tooling** — no compile/transpile step, works directly in the browser
- **Hot-reloadable** — edit the `.vue` file and refresh the browser

To add a new component, create a `component-name.template.vue` file and load it the same way:

```javascript
var res = await fetch("/static/js/app/component-name.template.vue");
var ComponentDef = { template: await res.text() /* data, methods... */ };
app.component("component-name", ComponentDef);
```

### Style Guide

Based on [Google JavaScript Style Guide](https://google.github.io/styleguide/jsguide.html):

1. Use `const`/`let`, arrow functions, template literals
2. HTML IDs and classes use **kebab-case** with 2-3 feature keywords
3. JavaScript-only classes use `js-` prefix
4. No inline `<style>` or `<script>` — use external files
5. Use semantic HTML elements (`<button>` not `<div>`)

### Framework Usage

- **Vue 3** for reactive components (search, accounts, sync status)
- **Native fetch** for API calls — use async/await, handle `Response.ok` and errors
- Use Vue's `$nextTick` for post-render DOM access

### Mobile / Responsive

- **Bottom nav** (&lt; 768px): Search, Accounts, Import tabs; hidden on email detail view
- **Infinite scroll**: Intersection Observer + "Load more" button; appends next page of results
- **Email detail**: Prev/next buttons, swipe left (prev) / right (next), position count ("3 of 50"), back returns to search list

### CSS Guidelines

1. Use CSS custom properties (variables) for theming
2. Dark theme by default (see `:root` variables in `app.css`)
3. Avoid `!important` — add comments if unavoidable
4. Mobile-first responsive design with `@media` breakpoints
5. BEM-like naming: `.email-card`, `.email-subject`, `.email-meta-row`
6. Mobile: touch targets ≥ 44px, `env(safe-area-inset-*)` for notched devices

### Accessibility

- All interactive elements must be keyboard-accessible
- Use proper ARIA labels on icon-only buttons
- Ensure sufficient color contrast (WCAG AA)
- Form inputs must have associated `<label>` elements

## User Data Layout

Each user's data lives under `users/{uuidv7}/`:

| Path                             | Purpose                               |
| -------------------------------- | ------------------------------------- |
| `user.json`                      | User metadata (name, email, provider) |
| `accounts.yml`                   | Email account configurations          |
| `sync.sqlite`                    | Sync jobs, UIDs, state                |
| `logs/{job-id}.jsonl`            | Structured sync logs                  |
| `{domain}/{local}/`              | Downloaded .eml files                 |
| `{domain}/{local}/index.parquet` | Search index per account              |

### Email Storage

Emails are stored as raw `.eml` files preserving RFC 822 format:

```
users/{uuid}/gmail.com/eslider/inbox/a1b2c3d4e5f67890-12345.eml
```

- Filename: `{sha256-prefix-16}-{uid}.eml`
- File mtime: set from email Date header (fallback: fuzzy Date parsing → Received header)
- Deduplication: by content checksum (IMAP/POP3) or message ID (Gmail)
- Use `./mails fix-dates` to batch-repair mtime on all existing .eml files

### PST Import Storage

PST/OST imports use the same structure and naming as .eml files:

- Emails: `{checksum}-{id}.eml` (RFC 822)
- Contacts: `{checksum}-{id}.vcf` (vCard 3.0)
- Calendars: `{checksum}-{id}.ics` (iCalendar 2.0)
- Notes: `{checksum}-{id}.txt` (plain text, from folder names containing "note")

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

| Method | Path                        | Description      |
| ------ | --------------------------- | ---------------- |
| GET    | `/login`                    | Login page       |
| GET    | `/auth/{provider}`          | Start OAuth flow |
| GET    | `/auth/{provider}/callback` | OAuth callback   |
| POST   | `/logout`                   | End session      |

### User

| Method | Path      | Description       |
| ------ | --------- | ----------------- |
| GET    | `/api/me` | Current user info |

### Accounts

| Method | Path                 | Description         |
| ------ | -------------------- | ------------------- |
| GET    | `/api/accounts`      | List email accounts |
| POST   | `/api/accounts`      | Add new account     |
| PUT    | `/api/accounts/{id}` | Update account      |
| DELETE | `/api/accounts/{id}` | Remove account      |

### Sync

| Method | Path               | Description                                   |
| ------ | ------------------ | --------------------------------------------- |
| POST   | `/api/sync`        | Trigger sync (all or specific account)        |
| POST   | `/api/sync/stop`   | Cancel a running sync (requires `account_id`) |
| GET    | `/api/sync/status` | Sync status per account (progress, errors)    |

### Import

| Method | Path                      | Description                                |
| ------ | ------------------------- | ------------------------------------------ |
| POST   | `/api/import/pst`         | Upload and import PST/OST file (multipart) |
| GET    | `/api/import/status/{id}` | Import job progress (phase, count)         |

### Search

| Method | Path                                  | Description             |
| ------ | ------------------------------------- | ----------------------- |
| GET    | `/api/search?q=&limit=&offset=&mode=` | Search emails           |
| GET    | `/api/email?path=`                    | Get single email detail |
| GET    | `/api/stats`                          | Index statistics        |
| POST   | `/api/reindex`                        | Rebuild search index    |

### Health

| Method | Path      | Description                     |
| ------ | --------- | ------------------------------- |
| GET    | `/health` | Health check (no auth required) |

## Common Issues

- **OAuth redirect mismatch:** Ensure `BASE_URL` matches the registered redirect URI in your OAuth app settings.
- **IMAP connection refused:** Check host, port, and SSL settings. Gmail requires an App Password (not regular password).
- **Search returns 0 results:** Run reindex after syncing new emails.
- **SQLite busy:** Increase `_busy_timeout` or reduce concurrent sync jobs.
