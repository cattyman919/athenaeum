// Package embed adapts Ollama's embedding API (anti-corruption layer,
// docs/01 §5, docs/06 §2).
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// nomic-embed-text task prefixes (recommended by the model card).
const (
	documentPrefix = "search_document: "
	queryPrefix    = "search_query: "
)

// Ollama is an embedder backed by POST {base}/api/embed.
type Ollama struct {
	base  string
	model string
	dim   int
	http  *http.Client
}

// NewOllama builds the adapter; each call has a 10 s timeout (docs/01 §5).
func NewOllama(baseURL, modelName string, dim int) *Ollama {
	return &Ollama{
		base: strings.TrimRight(baseURL, "/"), model: modelName, dim: dim,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Model returns the configured model name.
func (o *Ollama) Model() string { return o.model }

// EmbedDocuments embeds passages to be indexed.
func (o *Ollama) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	in := make([]string, len(texts))
	for i, t := range texts {
		in[i] = documentPrefix + t
	}
	return o.embed(ctx, in)
}

// EmbedQuery embeds a search query.
func (o *Ollama) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vs, err := o.embed(ctx, []string{queryPrefix + text})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

func (o *Ollama) embed(ctx context.Context, input []string) ([][]float32, error) {
	if len(input) == 0 {
		return nil, nil
	}
	unavailable := func(err error) error {
		return model.Wrap(model.CodeEmbeddingUnavailable, "Embedding service unavailable.", err)
	}
	body, _ := json.Marshal(map[string]any{"model": o.model, "input": input})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, unavailable(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, unavailable(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, unavailable(fmt.Errorf("ollama status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg))))
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, unavailable(fmt.Errorf("decode: %w", err))
	}
	if len(out.Embeddings) != len(input) {
		return nil, unavailable(fmt.Errorf("got %d embeddings for %d inputs", len(out.Embeddings), len(input)))
	}
	for i, v := range out.Embeddings {
		if len(v) != o.dim {
			return nil, unavailable(fmt.Errorf("embedding %d has %d dims, want %d (OLLAMA_EMBED_DIM)", i, len(v), o.dim))
		}
	}
	return out.Embeddings, nil
}
