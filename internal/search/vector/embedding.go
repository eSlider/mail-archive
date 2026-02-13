package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxTextLen = 2000
const batchSize = 8

// Embedder generates vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaEmbedder calls Ollama's /api/embed endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaEmbedder creates an embedder for the given Ollama server.
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type ollamaEmbedReq struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"`
}

type ollamaEmbedResp struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed generates embeddings for each text.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	inputs := make([]string, len(texts))
	for i, t := range texts {
		s := strings.TrimSpace(t)
		if len(s) > maxTextLen {
			s = s[:maxTextLen]
		}
		if s == "" {
			s = " "
		}
		inputs[i] = s
	}

	var result [][]float32
	for i := 0; i < len(inputs); i += batchSize {
		end := i + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]
		vecs, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embed batch %d: %w", i/batchSize, err)
		}
		result = append(result, vecs...)
	}
	return result, nil
}

func (e *OllamaEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(ollamaEmbedReq{Model: e.model, Input: texts})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed %s: %s", resp.Status, string(b))
	}

	var out ollamaEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	vecs := make([][]float32, len(out.Embeddings))
	for i, v := range out.Embeddings {
		vecs[i] = make([]float32, len(v))
		for j, x := range v {
			vecs[i][j] = float32(x)
		}
	}
	return vecs, nil
}
