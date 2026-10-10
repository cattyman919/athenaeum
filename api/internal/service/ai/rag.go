package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// RAG query-side parameters (docs/06 §2).
const (
	RAGTopK     = 8
	RAGPerBook  = 4
	RAGMinScore = 0.15
	snippetLen  = 240
)

// Embedder embeds the user question (embed.Ollama, nomic "search_query:" prefix).
type Embedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// ChunkStore retrieves description chunks by vector (repository.ChatRepository).
type ChunkStore interface {
	RetrieveChunks(ctx context.Context, vec []float32, k, perBook int, minScore float64) ([]model.RAGChunk, error)
}

// retrieve embeds the question and returns the top-K findings.
func retrieve(ctx context.Context, emb Embedder, store ChunkStore, question string) ([]model.RAGChunk, error) {
	vec, err := emb.EmbedQuery(ctx, question)
	if err != nil {
		return nil, err
	}
	return store.RetrieveChunks(ctx, vec, RAGTopK, RAGPerBook, RAGMinScore)
}

// snippet trims a chunk to a citation-card snippet on a word boundary.
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= snippetLen {
		return s
	}
	cut := string(r[:snippetLen])
	if i := strings.LastIndexByte(cut, ' '); i > snippetLen/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// findingsBlock renders the numbered findings appended to the system prompt; the
// agent cites them as [doc#N].
func findingsBlock(chunks []model.RAGChunk) string {
	if len(chunks) == 0 {
		return "Catalog findings: none retrieved for this question — use search_catalog."
	}
	var b strings.Builder
	b.WriteString("Catalog findings (cite as [doc#N] when used):\n")
	for i, c := range chunks {
		path := "no shelf location"
		if c.Path != nil {
			path = "shelf " + *c.Path
		}
		fmt.Fprintf(&b, "[doc#%d] book_id=%s \"%s\" (%s): %s\n", i+1, c.BookID, c.Title, path, snippet(c.Content))
	}
	return b.String()
}
