# mails

Local email archival tool. Syncs emails from multiple accounts (IMAP, POP3, Gmail API) into a plain file structure, runs as a Docker container on a schedule.

## Result structure

```
config/
  my-gmail.yml            # one YAML per account
  work-imap.yml

emails/
  my-gmail/
    inbox/
      a1b2c3d4e5f67890-4231.eml
      b2c3d4e5f6789012-4232.eml
    sent/
      c3d4e5f6789012345-4240.eml
    draft/
      ...
  work-imap/
    inbox/
      d4e5f67890123456-1234.eml
    sent/
      ...
```

Each filename is `{checksum}-{external-id}.eml` (checksum=SHA-256 of content; external-id=server UID). File mtime is set from the email's Date header. Folders: inbox, sent, draft, trash, spam.

Each `.eml` is a raw RFC 822 message — openable by any mail client (Thunderbird, mutt, etc.).

## Quick start

Prerequisites: Docker and Docker Compose.

```bash
# 1. Create config for your account
cp config/example.yml config/my-gmail.yml
nano config/my-gmail.yml

# 2. Ensure emails dir is writable by the container
mkdir -p emails && (sudo chown -R $(id -u):$(id -g) emails 2>/dev/null || true)

# 3. Start the sync daemon (or run one-off sync)
docker compose up -d

# 4. Watch logs
docker compose logs -f mail-sync
```

### One-off sync (single account)

```bash
# Sync just one account (e.g. eslider-gmail)
docker compose run --rm sync-one eslider-gmail
```

Use your host UID/GID so files are owned correctly:
```bash
DOCKER_UID=$(id -u) DOCKER_GID=$(id -g) docker compose run --rm sync-one eslider-gmail
```

## Account config

Each file in `config/` is a YAML describing one mail account. The config loader picks up every `*.yml` file (except `example.yml`).

### IMAP

```yaml
type: IMAP
email: user@gmail.com
password: your-app-password
host: imap.gmail.com
port: 993
ssl: true
folders:
  - INBOX
  - "[Gmail]/Sent Mail"
  - "[Gmail]/All Mail"
sync:
  interval: 5m
```

### POP3

```yaml
type: POP3
email: user@example.com
password: your-password
host: pop.example.com
port: 995
ssl: true
sync:
  interval: 10m
```

### Gmail API (OAuth2)

```yaml
type: GMAIL_API
email: user@gmail.com
credentials_file: /app/secrets/credentials.json
sync:
  interval: 5m
```

Setup steps for Gmail API:

1. Create a project at [Google Cloud Console](https://console.cloud.google.com/)
2. Enable the **Gmail API**
3. Create **OAuth 2.0 Client ID** credentials (Desktop app)
4. Download `credentials.json` into the `secrets/` directory
5. On first run the daemon will open a browser for authorization; the resulting token is saved to `secrets/` and reused automatically

### Sync interval format

The `interval` field accepts combinations of: `30s`, `5m`, `1h`, `1d` (e.g. `1h30m`). Default is `5m`.

## Gmail with App Password (IMAP)

Simplest way to sync Gmail — no API project needed:

1. Enable 2-Step Verification on your Google account
2. Go to <https://myaccount.google.com/apppasswords>
3. Generate an app password for "Mail"
4. Use it as `password` in an IMAP config with `host: imap.gmail.com`

## Environment variables (sync service)

Set in `docker-compose.yml` under `environment`:

| Variable | Default | Description |
|---|---|---|
| `TZ` | `Europe/Berlin` | Container timezone |
| `LOG_LEVEL` | `INFO` | Python log level (`DEBUG`, `INFO`, `WARNING`, `ERROR`) |
| `EMAILS_DIR` | `/app/emails` | Email storage path inside container |
| `CONFIG_DIR` | `/app/config` | Config directory path inside container |
| `SECRETS_DIR` | `/app/secrets` | Secrets directory path inside container |
| `SYNC_API_PORT` | `8081` | Port for the sync trigger HTTP API |

## How sync works

- **IMAP** — tracks downloaded UIDs in a `.sync_state` file per account; only fetches new messages on each run
- **POP3** — POP3 has no stable UIDs, so messages are deduplicated by SHA-256 hash of the raw content
- **Gmail API** — uses Gmail's native message IDs for deduplication

All state is plain text files inside the account's `emails/{account}/` directory. No database required.

## Run sync for a single account

```bash
# From project root (use venv or docker)
python scripts/sync_one.py eslider-gmail
```

## Migrating existing emails

**From date subdirs to flat:** `emails/{account}/{date}/{id}_{subject}.eml` → flat
```bash
python scripts/migrate_flat_emails.py --dry-run
python scripts/migrate_flat_emails.py
```

**From flat to folder structure:** flat files → `emails/{account}/inbox/`
```bash
python scripts/migrate_to_folders.py --dry-run
python scripts/migrate_to_folders.py
```

**To new naming ({checksum}-{id}.eml) and set mtime from Date header:**
```bash
python scripts/migrate_checksum_names.py --dry-run
python scripts/migrate_checksum_names.py
```

## Email search

A Go application (`search/`) indexes all `.eml` files into a DuckDB-backed index (persisted as Parquet with zstd compression) and provides full-text search via CLI and a web UI.

Features:
- **Case-insensitive substring search** across subject and body text
- **Checksum-based deduplication** — same email in inbox/allmail/sent is indexed once
- **Persistent Parquet index** — first run parses all `.eml` files (~17s for 34k emails), subsequent starts load from Parquet in ~1s
- **Paginated results** with highlighted snippets
- **Email detail view** with HTML rendering, plain text, and attachment list
- **Charset decoding** — handles KOI8-R, ISO-8859-1, Windows-1252 and other legacy encodings in headers and bodies
- **Sync trigger button** — trigger email sync from the web UI (requires `mail-sync` container)
- **Auto-reindex** after sync completes
- **Live development** with `docker compose watch`

### CLI usage

```bash
cd search
go build -o mail-search .

# Search by subject and body
./mail-search search "invoice"

# Show index stats
./mail-search stats

# Start HTTP server with web UI on :8080
./mail-search serve
```

### Docker

The search service is included in `docker-compose.yml`:

```bash
# Start the search UI
docker compose up -d mail-search
# Open http://localhost:8080

# Auto-rebuild on Go source changes
docker compose watch mail-search
```

### Environment variables (search service)

| Variable | Default | Description |
|---|---|---|
| `EMAILS_DIR` | `../emails` | Path to emails directory |
| `INDEX_PATH` | `index.parquet` | Path to Parquet index file (zstd compressed) |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `SYNC_URL` | _(empty)_ | URL of the sync trigger API (e.g. `http://mail-sync:8081`) |

### HTTP API

| Endpoint | Method | Description |
|---|---|---|
| `/api/search?q=keyword&limit=50&offset=0` | GET | Search emails (case-insensitive substring in subject + body) |
| `/api/email?path=account/inbox/file.eml` | GET | Get full email content for preview |
| `/api/stats` | GET | Index statistics |
| `/api/reindex` | POST | Rebuild index from `.eml` files and save to Parquet |
| `/api/accounts` | GET | List configured sync accounts and their status |
| `/api/sync` | POST | Trigger email sync (all accounts or `{"account":"name"}`) |
| `/health` | GET | Health check |

## Project structure

```
mails/
├── config/                  # Account configs (*.yml)
│   └── example.yml          # Template — copy and edit
├── emails/                  # Downloaded .eml files (gitignored)
├── data/                    # Parquet index (gitignored)
├── secrets/                 # OAuth tokens, credentials.json (gitignored)
├── scripts/
│   ├── daemon.py            # Main scheduler loop
│   ├── sync_api.py          # HTTP trigger API for on-demand sync
│   ├── config_loader.py     # YAML config parser + validator
│   ├── sync_imap.py         # IMAP sync via IMAPClient
│   ├── sync_pop3.py         # POP3 sync via stdlib poplib
│   └── sync_gmail_api.py    # Gmail API sync via google-api-python-client
├── search/                  # Go search application
│   ├── main.go              # CLI + HTTP server + web UI
│   ├── eml/parser.go        # .eml parser with charset decoding
│   ├── index/index.go       # DuckDB index with Parquet persistence
│   ├── Dockerfile
│   ├── go.mod
│   └── go.sum
├── docker-compose.yml
├── Dockerfile               # Python sync daemon
└── requirements.txt
```

## Development

See [TODO.md](TODO.md) for current status and open tasks.
See [AGENTS.md](AGENTS.md) for architecture decisions and coding guidelines.
