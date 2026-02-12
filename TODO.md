# TODO — Mail Sync Project

## Done

- [x] **Project structure** — established directory layout: `config/`, `emails/`, `scripts/`, `secrets/`
- [x] **Docker Compose** — single-service `mail-sync` container with volume mounts for config, emails, and secrets
- [x] **Dockerfile** — Python 3.12-slim base with `tini` init, pip dependencies, daemon entrypoint
- [x] **IMAP sync** (`scripts/sync_imap.py`) — full IMAP client using `imapclient`, batched UID-based downloads, deduplication via `.sync_state` file, stores as `emails/{account}/{YYYY-MM-DD}_{HH}_{MM}_{uid}_{subject}.eml`
- [x] **POP3 sync** (`scripts/sync_pop3.py`) — POP3/POP3S client using stdlib `poplib`, SHA-256 hash dedup (POP3 has no stable UIDs), same storage format
- [x] **Gmail API sync** (`scripts/sync_gmail_api.py`) — OAuth2 flow via `google-auth-oauthlib`, downloads raw RFC822 messages through Gmail REST API, token persistence in `secrets/`
- [x] **Config loader** (`scripts/config_loader.py`) — reads `config/*.yml`, validates type/email fields, parses human-readable intervals (`5m`, `1h`, `1d`)
- [x] **Daemon / scheduler** (`scripts/daemon.py`) — loads all account configs, schedules periodic sync per account via `schedule` lib, graceful SIGTERM/SIGINT shutdown
- [x] **Example config** (`config/example.yml`) — documented template covering IMAP, POP3, and GMAIL_API types
- [x] **`.gitignore`** — excludes `emails/`, `secrets/`, account configs (only `example.yml` tracked)
- [x] **`requirements.txt`** — pinned dependencies: `imapclient`, `pyyaml`, `schedule`, `google-api-python-client`, `google-auth-oauthlib`

## Not Yet Done

- [ ] Unit tests for sync modules and config loader
- [ ] Integration test with a local IMAP server (e.g. GreenMail in Docker)
- [ ] Gmail API OAuth2 initial authorization flow (requires browser, not headless-friendly yet)
- [ ] Attachment extraction to separate directory
- [ ] Email body indexing / full-text search
- [ ] Thunderbird profile import (read from `~/.thunderbird/` Maildir/mbox)
- [ ] Web UI for browsing downloaded emails
- [ ] Health check endpoint for Docker
- [ ] Notification on sync errors (webhook, email alert)
- [ ] Log rotation / structured JSON logging
- [ ] Multi-architecture Docker image build (amd64/arm64)
