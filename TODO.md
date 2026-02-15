# TODO — Mail Archive & Search Project

## Current Status (2026-02)

### Done

**Sync pipeline**

- [x] IMAP, POP3, Gmail API sync with config-driven scheduler
- [x] PST/OST import (go-pst, readpst fallback, streaming upload; .eml/.vcf/.ics/.txt)
- [x] DuckDB + Parquet index for keyword search (substring in subject/body)
- [x] Go search app with CLI + web UI, pagination, email detail view
- [x] GreenMail e2e tests (IMAP/POP3 sync, index, vector) and CI profile
- [x] Health check endpoint (`/health`)

**Vector / similarity search**

- [x] Qdrant vector DB service in docker-compose
- [x] Ollama integration for embeddings (host Ollama via http://172.17.0.1:11434)
- [x] Embedding pipeline: subject + body → sentence-transformers/all-MiniLM-L6-v2 → 384-dim vectors
- [x] Dynamic embedding dimension from Ollama; auto-recreate Qdrant collection on model switch
- [x] Chunked indexing (500 emails per chunk) for progress and memory control
- [x] Keyword / Similarity mode toggle in web UI
- [x] `mail-search` uses `network_mode: host` to reach host Ollama

### In progress / needs verification

- [ ] **Vector index population** — first full reindex (34k emails) may take 30–60 min; verify completion and similarity search
- [ ] **Ollama reachability from Docker** — when not using `network_mode: host`, ensure Ollama binds to `0.0.0.0` (e.g. `OLLAMA_HOST=0.0.0.0`) for `host.docker.internal` access

---

## GPU & Embedding Architecture

| Component           | Status                                                |
| ------------------- | ----------------------------------------------------- |
| **GPU**             | Not detected (no `nvidia-smi`)                        |
| **Ollama backend**  | CPU (or system default)                               |
| **Embedding model** | `all-minilm` (sentence-transformers/all-MiniLM-L6-v2) |
| **Embedding dims**  | 384                                                   |
| **Context length**  | 512 tokens                                            |

### Tokenizer / limits

- **Architecture:** sentence-transformers (MiniLM)
- **Tokenizer:** model-internal; Ollama handles tokenization
- **Limits:** We truncate input to 2000 chars (`maxTextLen`) to stay under 512-token context
- **Batch size:** 8 texts per Ollama `/api/embed` call to avoid context-length errors

---

## Not Yet Done

### High priority

- [ ] Unit tests for sync modules (imap, pop3, gmail, state) and account config loader
- [ ] Confirm vector reindex completes successfully end-to-end

### Medium priority

- [ ] Gmail OAuth2 headless flow (device code or host-side authorize script)
- [ ] Thunderbird profile import (mbox/Maildir)

### Low priority

- [ ] Attachment extraction to separate directory
- [ ] Full-text search index (beyond DuckDB substring) — SQLite FTS5 or Meilisearch
- [ ] Notification on sync errors (webhook/email)
- [ ] Multi-arch Docker (amd64/arm64)
- [ ] GPU support for Ollama when NVIDIA available (pass `--gpus all` to ollama container)

## Raw ideas

- **Ingest mail from everywhere** — Outlook PST/OST, Thunderbird, Bat Mailer, ZIP archives — then index for:
  - **Search:** subject, fuzzy match, filters by address (BCC, CC, From, To), companies, persons
  - **Semantic search:** similarity search over contacts and content
- **Geo search:** parse addresses from files, render on a map
- **ETL pipeline:** normalize to RFC/ISO formats, then push into CRMs via webhooks by job-task-process profile mechanics

---

## Future: GPU & Nomic

If you add an NVIDIA GPU and want to use it:

- Run Ollama with `--gpus all` (or `runtime: nvidia` in compose)
- Ollama will use CUDA when available

To use Nomic instead of all-minilm (768 dims, better quality):

- `ollama pull theepicdev/nomic-embed-text:v1.5-q6_K` or `nomic-embed-text`
- Set `EMBED_MODEL` to the model name, restart mail-search, reindex (dimension is detected automatically)
