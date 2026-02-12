# Contributing

See [AGENTS.md](AGENTS.md) for architecture decisions and coding guidelines. This guide covers API usage, Qdrant verification, and similarity search.

## Verify Qdrant

Qdrant runs on port 6333 (REST) and 6334 (gRPC). With `docker compose up`, it's available at `localhost`.

**Check Qdrant is running:**
```bash
curl -s http://localhost:6333/collections
# {"result":{"collections":[{"name":"mail_emails"}]},"status":"ok",...}
```

**Check collection status:**
```bash
curl -s http://localhost:6333/collections/mail_emails
# Shows: status, points_count, config (vector size, distance)
```

**Collection must have vectors for similarity search.** If `points_count` is 0, run a reindex (see below). Expect ~25 emails/sec for vector indexing; 34k emails ≈ 23 minutes.

**Dimension mismatch:** If vector size is 768 but you use `all-minilm` (384 dims), delete and recreate:
```bash
curl -X DELETE http://localhost:6333/collections/mail_emails
curl -X PUT http://localhost:6333/collections/mail_emails -H "Content-Type: application/json" -d '{"vectors":{"size":384,"distance":"Cosine"}}'
# Then reindex
curl -X POST http://localhost:8080/api/reindex
```

## HTTP API Examples

Base URL: `http://localhost:8080` (mail-search service)

### Health check
```bash
curl -s http://localhost:8080/health
```

### Search (keyword mode)

Case-insensitive substring search in subject and body. Default when `mode` is omitted.

```bash
# Basic search
curl -s "http://localhost:8080/api/search?q=fabrik"

# With pagination
curl -s "http://localhost:8080/api/search?q=invoice&limit=20&offset=0"

# Empty query returns all emails, newest first
curl -s "http://localhost:8080/api/search?q=&limit=10"
```

**Response:**
```json
{
  "query": "fabrik",
  "total": 81,
  "offset": 0,
  "limit": 50,
  "hits": [
    {
      "path": "eslider-gmail/allmail/d6119262cb5747a1-146874.eml",
      "subject": "...",
      "from": "...",
      "to": "...",
      "date": "2023-11-01T19:29:28Z",
      "size": 118625,
      "snippet": "...highlighted excerpt..."
    }
  ],
  "indexed_at": "2026-02-12T15:29:00Z"
}
```

### Search (similarity mode)

Semantic search via Qdrant + Ollama embeddings. Requires `QDRANT_URL` and `OLLAMA_URL` to be set, and a populated vector index (run reindex first).

```bash
curl -s "http://localhost:8080/api/search?q=project+deadline&mode=similarity&limit=20"
```

**Prerequisites:**
- Ollama running with embedding model: `ollama pull all-minilm`
- Vector index populated: `curl -X POST http://localhost:8080/api/reindex` (run once, takes ~25 emails/sec)
- Qdrant collection dimension must match model (384 for all-minilm). Mismatch → delete collection and reindex.

**Response:** Same shape as keyword search; similarity mode orders by relevance score.

### Get single email
```bash
# path is URL-encoded, from search hit
curl -s "http://localhost:8080/api/email?path=eslider-gmail%2Fallmail%2Fabc123-123.eml"
```

### Index stats
```bash
curl -s http://localhost:8080/api/stats
# {"total_emails":34229,"indexed_at":"...","similarity_available":true,...}
```

### Reindex

Rebuild Parquet + vector index from `.eml` files. Returns `202 Accepted` and runs in background; use status endpoint to poll progress.

```bash
# Start reindex
curl -s -X POST http://localhost:8080/api/reindex
# 202 {"status":"started"}

# Poll progress
curl -s http://localhost:8080/api/reindex/status
# {"running":true,"phase":"vector","vec_indexed":5000,"vec_total":34229,...}
```

If `running` is true, poll every 2–3 seconds until done.

### Sync (forwarded to mail-sync)
```bash
# List accounts
curl -s http://localhost:8080/api/accounts

# Trigger sync for all accounts
curl -s -X POST http://localhost:8080/api/sync -H "Content-Type: application/json" -d '{}'

# Sync single account
curl -s -X POST http://localhost:8080/api/sync -H "Content-Type: application/json" -d '{"account":"eslider-gmail"}'
```

## Qdrant REST API (direct)

For debugging or custom integrations, Qdrant exposes its own API:

| Endpoint | Description |
|----------|-------------|
| `GET /collections` | List collections |
| `GET /collections/{name}` | Collection info (points, config) |
| `DELETE /collections/{name}` | Delete collection |
| `PUT /collections/{name}` | Create collection (body: `{"vectors":{"size":384,"distance":"Cosine"}}`) |

```bash
# Create empty collection (384 dims for all-minilm)
curl -X PUT http://localhost:6333/collections/mail_emails \
  -H "Content-Type: application/json" \
  -d '{"vectors":{"size":384,"distance":"Cosine"}}'
```

## Common issues

- **Similarity search returns 0 or error:** Vector index empty or dimension mismatch. Run reindex or fix collection dim (see Verify Qdrant).
- **`connection refused` on 6333/6334:** Qdrant not running. `docker compose up -d qdrant`.
- **`similarity search failed`:** Check mail-search logs; often dimension mismatch (Qdrant expects 768, model returns 384). Delete Qdrant collection and reindex.
