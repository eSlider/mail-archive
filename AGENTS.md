# AGENTS.md

Multi-user email archive: IMAP/POP3/Gmail → `users/{uuid}/{domain}/{local}/{folder}/`. Optional S3 (MinIO) for blobs. OAuth2 + bcrypt, UUIDv7, DuckDB+Parquet search.

**Critical:** NEVER delete or mark emails read on server. Sync is read-only.

**Docs:** [CONTRIBUTING.md](CONTRIBUTING.md) — architecture, API, coding standards, testing. [TODO.md](TODO.md) — roadmap. [README.md](README.md) — config, env vars.
