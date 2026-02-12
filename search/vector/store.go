package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eslider/mails/search/eml"
	"github.com/qdrant/go-client/qdrant"
)

const collectionName = "mail_emails"
const vectorDistance = qdrant.Distance_Cosine

// Store manages the Qdrant vector index for email similarity search.
type Store struct {
	client     *qdrant.Client
	embedder   Embedder
	vectorSize int
	restHost   string // Qdrant REST base URL (e.g. http://localhost:6333)
}

// NewStore creates a Qdrant store. qdrantAddr is e.g. "localhost:6334" or "http://qdrant:6334".
// Fetches embedding dimension from Ollama so Qdrant collection matches the configured model.
func NewStore(qdrantAddr, ollamaURL, embedModel string) (*Store, error) {
	host, port, err := parseHostPort(qdrantAddr)
	if err != nil {
		return nil, err
	}

	client, err := qdrant.NewClient(&qdrant.Config{
		Host: host,
		Port: int(port),
	})
	if err != nil {
		return nil, err
	}

	embedder := NewOllamaEmbedder(ollamaURL, embedModel)
	// Fetch actual dimension from Ollama so collection always matches the model.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	vecs, err := embedder.Embed(ctx, []string{"x"})
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("embed model %q: %w", embedModel, err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		client.Close()
		return nil, fmt.Errorf("embed model %q returned empty vector", embedModel)
	}
	dim := len(vecs[0])
	log.Printf("Embedding model %q: %d dimensions", embedModel, dim)

	restBase := "http://" + net.JoinHostPort(host, "6333")
	return &Store{client: client, embedder: embedder, vectorSize: dim, restHost: restBase}, nil
}

func parseHostPort(addr string) (string, int64, error) {
	addr = strings.TrimSpace(addr)
	if s := strings.TrimPrefix(addr, "http://"); s != addr {
		addr = s
	} else if s := strings.TrimPrefix(addr, "https://"); s != addr {
		addr = s
	}
	if host, portStr, err := net.SplitHostPort(addr); err == nil {
		port, _ := strconv.ParseInt(portStr, 10, 64)
		if port == 0 {
			port = 6334
		}
		return host, port, nil
	}
	u, err := url.Parse("//" + addr)
	if err != nil {
		return "", 0, err
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := int64(6334)
	if p := u.Port(); p != "" {
		port, _ = strconv.ParseInt(p, 10, 64)
	}
	return host, port, nil
}

// Close releases resources.
func (s *Store) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// collectionVectorSize returns the vector size of an existing collection, or 0 if it does not exist.
func (s *Store) collectionVectorSize(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.restHost+"/collections/"+collectionName, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("qdrant GET collection: %s", resp.Status)
	}
	var out struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size uint64 `json:"size"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return int(out.Result.Config.Params.Vectors.Size), nil
}

// EnsureCollection creates the collection if it does not exist, or recreates it if vector dimension mismatches.
func (s *Store) EnsureCollection(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, collectionName)
	if err != nil {
		return err
	}
	if exists {
		currentDim, err := s.collectionVectorSize(ctx)
		if err != nil {
			return err
		}
		if currentDim != s.vectorSize {
			log.Printf("Qdrant collection has %d dims, model has %d — recreating collection", currentDim, s.vectorSize)
			if err := s.client.DeleteCollection(ctx, collectionName); err != nil {
				return err
			}
		} else {
			return nil
		}
	}
	return s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig:  qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(s.vectorSize),
			Distance: vectorDistance,
		}),
	})
}

// RecreateCollection deletes and recreates the collection for a clean reindex.
func (s *Store) RecreateCollection(ctx context.Context) error {
	if exists, err := s.client.CollectionExists(ctx, collectionName); err == nil && exists {
		if err := s.client.DeleteCollection(ctx, collectionName); err != nil {
			return err
		}
	}
	return s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig:  qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(s.vectorSize),
			Distance: vectorDistance,
		}),
	})
}

// WalkEmailsFn is a function that walks an email directory and returns parsed emails.
type WalkEmailsFn func(emailDir string) ([]eml.Email, int)

// IndexProgressFunc is called during indexing with (indexed, total). Pass nil to skip.
type IndexProgressFunc func(indexed, total int)

// IndexEmails parses emails from the directory and indexes them into Qdrant.
func (s *Store) IndexEmails(ctx context.Context, emailDir string, walkFn WalkEmailsFn, progress IndexProgressFunc) (int, int, error) {
	emails, errCount := walkFn(emailDir)
	if len(emails) == 0 {
		return 0, errCount, nil
	}

	if err := s.RecreateCollection(ctx); err != nil {
		return 0, errCount, err
	}

	start := time.Now()
	log.Printf("Vector indexing %d emails in chunks...", len(emails))
	// Embed and upsert in chunks to limit memory and allow progress on partial completion.
	const chunkSize = 500
	var totalIndexed int
	for chunkStart := 0; chunkStart < len(emails); chunkStart += chunkSize {
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(emails) {
			chunkEnd = len(emails)
		}
		chunk := emails[chunkStart:chunkEnd]
		texts := make([]string, len(chunk))
		for i, e := range chunk {
			texts[i] = textToEmbed(e)
		}
		vecs, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			return totalIndexed, errCount, err
		}
		if len(vecs) != len(chunk) {
			return totalIndexed, errCount, fmt.Errorf("embed returned %d vectors for %d emails", len(vecs), len(chunk))
		}

		// Upsert this chunk.
		points := make([]*qdrant.PointStruct, len(chunk))
		for j, e := range chunk {
			points[j] = &qdrant.PointStruct{
				Id: qdrant.NewIDNum(pathToID(e.Path)),
				Vectors: newVector(vecs[j]),
				Payload: qdrant.NewValueMap(map[string]any{
					"path":   e.Path,
					"subject": e.Subject,
					"from":   e.From,
					"to":    e.To,
					"date":  e.Date.Unix(),
				}),
			}
		}
		wait := true
		if _, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: collectionName,
			Points:         points,
			Wait:           &wait,
		}); err != nil {
			return totalIndexed, errCount, err
		}
		totalIndexed += len(chunk)
		if progress != nil {
			progress(totalIndexed, len(emails))
		}
		elapsed := time.Since(start)
		rate := float64(totalIndexed) / elapsed.Seconds()
		log.Printf("Indexed %d/%d emails into Qdrant (%.1f emails/sec, %.1fs elapsed)",
			totalIndexed, len(emails), rate, elapsed.Seconds())
	}
	if totalIndexed > 0 {
		elapsed := time.Since(start)
		log.Printf("Vector index complete: %d emails in %.1fs (%.1f emails/sec)",
			totalIndexed, elapsed.Seconds(), float64(totalIndexed)/elapsed.Seconds())
	}
	return totalIndexed, errCount, nil
}

func pathToID(path string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(path))
	return h.Sum64()
}

func textToEmbed(e eml.Email) string {
	var b strings.Builder
	if e.Subject != "" {
		b.WriteString(e.Subject)
		b.WriteString("\n\n")
	}
	if e.BodyText != "" {
		b.WriteString(e.BodyText)
	}
	return b.String()
}

// newVector wraps []float32 for qdrant.PointStruct.Vectors.
func newVector(v []float32) *qdrant.Vectors {
	return qdrant.NewVectors(v...)
}

// SearchResult is a single similarity search hit.
type SearchResult struct {
	Path    string  `json:"path"`
	Subject string  `json:"subject"`
	From    string  `json:"from"`
	To      string  `json:"to"`
	Date    int64   `json:"date"`
	Score   float32 `json:"score"`
}

// Search runs a similarity search and returns hits with metadata.
func (s *Store) Search(ctx context.Context, query string, limit, offset int) ([]SearchResult, int, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, 0, nil
	}

	// Ensure collection exists and has correct dimension (recreates if model changed).
	if err := s.EnsureCollection(ctx); err != nil {
		return nil, 0, err
	}

	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, 0, err
	}
	if len(vecs) == 0 {
		return []SearchResult{}, 0, nil
	}

	// Query with Limit+1 to check if there are more (for total estimate).
	reqLimit := limit + offset + 1
	queryVector := vecs[0]

	// qdrant.NewQuery expects variadic float32. Convert slice.
	queryFloats := make([]float32, len(queryVector))
	copy(queryFloats, queryVector)

	hits, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(queryFloats...),
		Limit:          ptr(uint64(reqLimit)),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, 0, err
	}

	total := len(hits)
	// Apply offset and limit.
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := hits[start:end]

	results := make([]SearchResult, len(page))
	for i, p := range page {
		payload := p.GetPayload()
		if payload == nil {
			continue
		}
		results[i] = SearchResult{
			Path:    getPayloadStr(payload, "path"),
			Subject: getPayloadStr(payload, "subject"),
			From:    getPayloadStr(payload, "from"),
			To:      getPayloadStr(payload, "to"),
			Date:    getPayloadInt64(payload, "date"),
			Score:   float32(p.GetScore()),
		}
	}
	return results, total, nil
}

func ptr[T any](v T) *T { return &v }

func getPayloadStr(payload map[string]*qdrant.Value, key string) string {
	if v, ok := payload[key]; v != nil && ok {
		if s := v.GetStringValue(); s != "" {
			return s
		}
	}
	return ""
}

func getPayloadInt64(payload map[string]*qdrant.Value, key string) int64 {
	if v, ok := payload[key]; v != nil && ok {
		return v.GetIntegerValue()
	}
	return 0
}
