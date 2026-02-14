# Refactoring Analysis

## 1. search/ vs internal/search — Deduplication

### Current State
- **`search/`** — Standalone Go module (`github.com/eslider/mails/search`) with its own `main.go`. Single-user, flat `EMAILS_DIR`, proxies sync to external `mail-sync` via `SYNC_URL`. Has similarity search (Qdrant + Ollama).
- **`internal/search/`** — Used by main `mails` app. Multi-user, per-account parquet indices, `SearchMulti()` for cross-account search.

### Duplication
| Package | search/ | internal/search | Notes |
|---------|---------|-----------------|-------|
| eml/    | parser.go, parser_test.go | parser.go (no test) | Nearly identical |
| index/  | index.go, index_test.go   | index.go + SearchMulti, ClearCache, AccountID | internal extends for multi-account |
| vector/ | store.go, embedding.go    | store.go, embedding.go | ~95% identical, only import path differs |

### Recommendations

**Done: Consolidated into main module**
- `search/main.go` → `cmd/mail-search/main.go` (imports from `internal/search`)
- Tests migrated to `internal/search/eml/parser_test.go` and `internal/search/index/index_test.go`
- `search/` directory and `search/go.mod` removed

**Build standalone search binary:**
```bash
go build -o mail-search ./cmd/mail-search
./mail-search search "query"
./mail-search serve   # HTTP server with API + UI (set SYNC_URL to proxy to mail-sync)
./mail-search stats
```

**Option B (obsolete): Keep search/ as legacy**
- Add README in `search/` stating it's legacy; prefer main `mails` app.
- No code changes; duplication remains.

### Similarity Search in Main App
The main app's `handleSearch` in `internal/web/handlers.go` **does not use vector/similarity** — it only does keyword search. The frontend sends `mode=similarity` but the backend ignores it. `similarity_available` is returned in `/api/stats` when Qdrant+Ollama are configured.

**To enable similarity search in main app:**
- Create a shared `vector.Store` per user (or global with user filter in payload)
- In `handleSearch`, when `mode=similarity` and `vecStore != nil`, call `vecStore.Search()` and merge with or replace keyword results
- Vector store currently indexes a single dir; multi-account would need collection-per-user or `account_id` in Qdrant payload

---

## 2. Python Scripts (scripts/)

### Inventory
| Script | Purpose |
|--------|---------|
| `config_loader.py` | Load YAML account configs from `config/` |
| `sync_imap.py` | IMAP sync (replaced by Go `internal/sync/imap`) |
| `sync_pop3.py` | POP3 sync (replaced by Go `internal/sync/pop3`) |
| `sync_gmail_api.py` | Gmail API sync (replaced by Go `internal/sync/gmail`) |
| `sync_one.py` | Sync single account |
| `sync_api.py` | HTTP API for sync |
| `daemon.py` | Daemon/scheduler |
| `eml_utils.py` | .eml checksum, date parsing |
| `folder_mapping.py` | IMAP folder → path mapping |
| `list_imap_folders.py` | List IMAP folders |
| `migrate_*.py` | Data migrations (flat→hierarchical, checksum names, etc.) |
| `seed_test.py` | Test seeding |

### Decision
**Keep migration scripts only; remove sync/API scripts.**

- **Remove**: `sync_imap.py`, `sync_pop3.py`, `sync_gmail_api.py`, `sync_one.py`, `sync_api.py`, `daemon.py`, `eml_utils.py`, `folder_mapping.py`, `list_imap_folders.py`, `config_loader.py`, `seed_test.py` — all superseded by Go implementation or one-off.
- **Keep**: `migrate_flat_emails.py`, `migrate_hierarchical_folders.py`, `migrate_checksum_names.py`, `migrate_to_folders.py` — useful for one-time data migration from legacy layout to `users/{uuid}/` structure. Add a README in `scripts/` explaining their purpose and that they're for legacy migration only.

---

## 3. ollama-modelfiles/

### Contents
- `nomic-embed-q8.Modelfile` — `FROM /tmp/nomic-q8.gguf`
- `nomic-embed-text-q8.Modelfile` — 265 bytes

### Purpose
Ollama Modelfiles for custom embedding models. The default `EMBED_MODEL` is `all-minilm` (built-in). These files define alternatives (e.g. nomic-embed quantized) for users who want different embedding models.

### Decision
**Optional — keep for advanced users.**

- Not required for default operation.
- If you use `all-minilm` only, you can remove `ollama-modelfiles/`.
- If you want to offer nomic-embed or other custom models, keep the folder and add a short note in README or TODO.md:
  ```markdown
  ### Custom embedding models
  See `ollama-modelfiles/` for Modelfiles. Build with:
  `ollama create nomic-embed -f ollama-modelfiles/nomic-embed-q8.Modelfile`
  ```

---

## 4. main.js Sync Status Refactor (Done)

### Before
- `loadSyncStatus()` — fetched `/api/sync/status`, set `syncStatuses`
- `refreshSyncStatus()` — fetched same, set `syncStatuses` + `syncStatusMap` + error toasts
- `pollSyncStatus()` — interval fetching same, checked `anySyncing`, showed toast when done

### After
- `fetchAndApplySyncStatus(onApplied?)` — single function that fetches, updates state, and optionally calls `onApplied(list)` when done
- `loadSyncStatus()` — calls `fetchAndApplySyncStatus()`
- `refreshSyncStatus()` — calls `fetchAndApplySyncStatus()`
- `pollSyncStatus()` — interval calls `fetchAndApplySyncStatus(cb)` and uses callback to check `anySyncing` and clear interval

Duplication removed; one source of truth for sync status fetching.
