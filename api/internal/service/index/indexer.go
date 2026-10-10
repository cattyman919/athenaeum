package index

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// BatchSize is the number of chunks per embedding call (docs/08 §7).
const BatchSize = 8

// Store is the persistence the indexer needs (repository.IndexStore).
type Store interface {
	// Books returns books to index (all, or only bookID when non-empty).
	Books(ctx context.Context, bookID string) ([]model.IndexBook, error)
	// Replace atomically rewrites the embedding rows of the given books:
	// upserts by (book_id, chunk_index) and deletes leftover indexes.
	Replace(ctx context.Context, bookIDs []string, rows []model.ChunkRow) error
	// StaleBooks lists books having rows whose (model, chunker_version) differ.
	StaleBooks(ctx context.Context, modelName, chunkerVersion string) ([]model.StaleBook, error)
	// StaleChunkCount counts rows whose (model, chunker_version) differ.
	StaleChunkCount(ctx context.Context, modelName, chunkerVersion string) (int, error)
}

// Embedder produces document embeddings (embed.Ollama).
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
}

// Summary is the exit report of a reindex run.
type Summary struct {
	IndexedBooks  int
	ChunksWritten int
	StaleChunks   int
}

// Indexer chunks, embeds and stores book descriptions.
type Indexer struct {
	store Store
	emb   Embedder
	model string
	log   *slog.Logger
}

// New builds an Indexer for the given embedding model name.
func New(store Store, emb Embedder, modelName string, log *slog.Logger) *Indexer {
	return &Indexer{store: store, emb: emb, model: modelName, log: log}
}

// docText gives each chunk its book context so short blurbs still embed well.
func docText(b model.IndexBook, chunk string) string {
	if len(b.Authors) == 0 {
		return b.Title + ". " + chunk
	}
	return b.Title + " by " + strings.Join(b.Authors, ", ") + ". " + chunk
}

// Run reindexes every book (or only bookID). All embeddings are computed
// before any write, then stored in one transaction, so an embedding outage
// leaves the index untouched (docs/08 §8).
func (ix *Indexer) Run(ctx context.Context, bookID string) (Summary, error) {
	books, err := ix.store.Books(ctx, bookID)
	if err != nil {
		return Summary{}, err
	}
	type pending struct {
		book  model.IndexBook
		idx   int
		chunk string
	}
	var todo []pending
	ids := make([]string, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.ID)
		for i, c := range Chunk(b.Description) {
			todo = append(todo, pending{book: b, idx: i, chunk: c})
		}
	}

	rows := make([]model.ChunkRow, 0, len(todo))
	for start := 0; start < len(todo); start += BatchSize {
		end := min(start+BatchSize, len(todo))
		texts := make([]string, 0, end-start)
		for _, p := range todo[start:end] {
			texts = append(texts, docText(p.book, p.chunk))
		}
		vecs, err := ix.emb.EmbedDocuments(ctx, texts)
		if err != nil {
			return Summary{}, fmt.Errorf("embed batch starting at %d: %w", start, err)
		}
		for i, p := range todo[start:end] {
			rows = append(rows, model.ChunkRow{
				BookID: p.book.ID, ChunkIndex: p.idx, Content: p.chunk, Embedding: vecs[i],
				Model: ix.model, ChunkerVersion: ChunkerVersion,
			})
		}
	}
	if err := ix.store.Replace(ctx, ids, rows); err != nil {
		return Summary{}, err
	}
	stale, err := ix.store.StaleChunkCount(ctx, ix.model, ChunkerVersion)
	if err != nil {
		return Summary{}, err
	}
	ix.log.Info("reindex complete", "indexed_books", len(books), "chunks_written", len(rows), "stale_chunks", stale)
	return Summary{IndexedBooks: len(books), ChunksWritten: len(rows), StaleChunks: stale}, nil
}

// ModelCheck reports books whose rows were built with a different model or chunker.
func (ix *Indexer) ModelCheck(ctx context.Context) ([]model.StaleBook, error) {
	return ix.store.StaleBooks(ctx, ix.model, ChunkerVersion)
}
