# Measurements — Index Creation & Search Performance

## Environment (measured 2026-02)

| Component             | Value                                                 |
| --------------------- | ----------------------------------------------------- |
| **CPU**               | (system default)                                      |
| **GPU**               | None                                                  |
| **Embedding model**   | `all-minilm` (sentence-transformers/all-MiniLM-L6-v2) |
| **Vector dimensions** | 384                                                   |
| **Ollama**            | Host at 172.17.0.1:11434 (Docker bridge gateway)     |

---

## Parquet index (keyword search)

| Emails | Time            | Throughput      |
| ------ | --------------- | --------------- |
| 200    | < 1 s           | —               |
| ~65k   | ~10–15 s (est.) | ~5k+ emails/sec |

Parquet index build is dominated by disk I/O and DuckDB write. Typically completes in seconds even for tens of thousands of emails.

---

## Vector index (similarity search)

| Emails | Time           | Throughput          |
| ------ | -------------- | ------------------- |
| 200    | 7.9 s          | **25.2 emails/sec** |
| ~65k   | ~43 min (est.) | ~25 emails/sec      |

**Bottleneck:** Ollama embedding calls. Each email requires embedding subject + body text; batch size 8 per Ollama `/api/embed` request.

### Pipeline parameters

| Parameter          | Value                      |
| ------------------ | -------------------------- |
| Chunk size         | 500 emails                 |
| Batch size (embed) | 8 texts per Ollama request |
| Max text length    | 2000 chars                 |

---

## Reindex API response

`POST /api/reindex` returns timing fields after completion:

```json
{
  "indexed": 65100,
  "errors": 0,
  "vec_indexed": 65100,
  "parquet_secs": 12.3,
  "vector_secs": 2610.5,
  "total_secs": 2622.8
}
```

---

## Model handling

- **Dynamic dimension:** On startup, the store fetches the embedding dimension from Ollama for the configured `EMBED_MODEL`. No hardcoded vector size.
- **Auto-repair on mismatch:** If the Qdrant collection exists with a different vector size (e.g. after switching from nomic to all-minilm), it is recreated automatically before the next search. Reindex is required to repopulate.
- **Switch models:** Set `EMBED_MODEL` to the desired model (e.g. `theepicdev/nomic-embed-text:v1.5-q6_K` for 768 dims). Restart mail-search, run reindex. No code changes needed.

---

## Similarity search latency

| Operation | Typical |
| --------- | ------- |
| Embed query (1 text) | ~1–2 s (Ollama CPU) |
| Qdrant query | < 100 ms |
| End-to-end | ~1–2 s |

---

## Notes

- **GPU:** With NVIDIA GPU and `OLLAMA_GPU_LAYERS`, embedding throughput can increase significantly.
- **Model switch:** Nomic (768 dims) is slower per-embed but may yield better quality; expect lower emails/sec on CPU.
- **First run:** Qdrant collection is created on first reindex. Subsequent reindexes recreate the collection (delete + create) before upserting.
