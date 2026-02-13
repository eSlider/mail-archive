# AGENTS.md — Instructions for AI Agents Working on This Project

## Project Context

This is a **local email archival tool** that syncs emails from multiple accounts (IMAP, POP3, Gmail API) into a hierarchical structure: `emails/{account}/{path}/{checksum}-{external-id}.eml`. Path preserves IMAP folder hierarchy (e.g. `[Gmail]/Benachrichtigung/Kartina.TV` → `gmail/benachrichtigung/kartina_tv/`). Checksum is SHA-256 of content; file mtime from Date header.

## Architecture

```
mails/
├── config/          # Per-account YAML configs (*.yml)
├── emails/          # Downloaded .eml files (inbox/, sent/, draft/ per account)
├── secrets/         # OAuth tokens, credentials.json
├── scripts/
│   ├── daemon.py          # Main scheduler loop
│   ├── config_loader.py   # YAML config parsing
│   ├── sync_imap.py       # IMAP sync module
│   ├── sync_pop3.py       # POP3 sync module
│   └── sync_gmail_api.py  # Gmail API sync module
├── docker-compose.yml
├── Dockerfile
└── requirements.txt
```

## Key Design Decisions

- **UID-based dedup for IMAP** — `.sync_state` tracks downloaded UIDs per account
- **SHA-256 hash dedup for POP3** — POP3 protocol lacks stable UIDs
- **Message ID dedup for Gmail API** — uses Gmail's native message IDs
- **Storage: raw .eml** — preserves original RFC822 format, parseable by any mail client
- **No database** — state is a plain text file per account, emails are plain files

## Next Steps (Priority Order)

### 1. Tests (High Priority)

Write unit tests using `pytest`. Key areas:
- `config_loader.py`: interval parsing, validation, error handling
- `sync_imap.py`: mock `IMAPClient`, verify file creation and state tracking
- `sync_pop3.py`: mock `poplib`, verify hash dedup logic
- `sync_gmail_api.py`: mock Google API client
- Add a `tests/` directory, update `requirements.txt` with `pytest`
- Integration test: add a GreenMail (Java IMAP/POP3/SMTP test server) service to `docker-compose.yml` for end-to-end testing

### 2. Thunderbird Import (Medium Priority)

Read emails from local Thunderbird profile (`~/.thunderbird/<profile>/ImapMail/` or `Mail/`):
- Parse mbox files using Python's `mailbox.mbox`
- Parse Maildir using `mailbox.Maildir`
- Create a new sync type `THUNDERBIRD` in `config_loader.py`
- Config should point to the Thunderbird profile path
- Add volume mount in `docker-compose.yml` for Thunderbird profile (read-only)

### 3. Gmail OAuth2 Headless Flow (Medium Priority)

Current OAuth2 flow uses `run_local_server()` which requires a browser. For Docker:
- Implement device code flow (`flow.run_console()`) as alternative
- Or: create a one-time `scripts/authorize_gmail.py` CLI tool that runs on the host, saves token to `secrets/`, then the container reuses it
- Document the setup steps in README

### 4. Attachment Extraction (Low Priority)

After downloading `.eml`, optionally extract attachments:
- Add `extract_attachments: true` config option
- Store in `emails/{account}/attachments/{filename}`
- Use Python's `email` stdlib to walk MIME parts
- Skip inline images unless configured otherwise

### 5. Full-Text Search Index (Low Priority)

Enable searching across all downloaded emails:
- Option A: SQLite FTS5 index (lightweight, no extra service)
- Option B: Add a Meilisearch/Typesense container to `docker-compose.yml`
- Index: sender, recipient, subject, body text, date
- Provide a CLI query tool: `scripts/search.py "keyword"`

### 6. Web UI for Browsing (Low Priority)

Add a lightweight web interface:
- FastAPI or Flask backend serving the `emails/` directory
- Parse `.eml` files on-the-fly for display
- Simple HTML/CSS viewer (no heavy JS frameworks needed)
- Add as a second service in `docker-compose.yml`
- Search integration if index exists

### 7. Health Check & Monitoring (Low Priority)

- Add a `/health` HTTP endpoint (tiny HTTP server in daemon)
- Report: last sync time per account, message counts, errors
- Add `healthcheck` to `docker-compose.yml`
- Optional: webhook notifications on sync failure

## Coding Guidelines

- NEVER REMOVE EMAILS from server.
- **Python 3.12+**, strict typing with `from __future__ import annotations`
- **No classes unless necessary** — prefer pure functions
- **Minimal dependencies** — stdlib first, then well-maintained PyPI packages
- **Comments in English only**
- **Test first** — write the test, then implement
- All configs in YAML, all state in plain text files
- Keep sync modules independent — each handles one protocol
- Log everything at INFO level, errors with full tracebacks
