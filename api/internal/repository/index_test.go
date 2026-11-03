package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

type constEmb struct{}

func (constEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, 768)
		v[i%768] = 1
		out[i] = v
	}
	return out, nil
}

func TestReindexStoreUpsertPruneAndStale(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fx, _ := seed.Load("../../mocks")
	if _, err := seed.Run(ctx, repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}
	st := repository.NewIndexStore(pool)
	ix := index.New(st, constEmb{}, "nomic-embed-text", logging.Discard())

	sum, err := ix.Run(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.IndexedBooks != 39 || sum.ChunksWritten != 39 || sum.StaleChunks != 0 {
		t.Fatalf("%+v", sum)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_embeddings`).Scan(&rows)
	if rows != 39 {
		t.Fatalf("rows %d", rows)
	}

	// Idempotent: rerun keeps 39 rows.
	if _, err := ix.Run(ctx, ""); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_embeddings`).Scan(&rows)
	if rows != 39 {
		t.Fatalf("rows after rerun %d", rows)
	}

	// Prune: a stray chunk index is removed when the description has one chunk.
	var id string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM books WHERE title = 'Dune'`).Scan(&id)
	if _, err := pool.Exec(ctx,
		`INSERT INTO book_embeddings (book_id, chunk_index, content, embedding, model, chunker_version)
		 VALUES ($1::uuid, 5, 'stray', $2::vector, 'old-model', 'v0')`, id, repository.VectorLiteral(make([]float32, 768))); err != nil {
		t.Fatal(err)
	}
	stale, _ := st.StaleBooks(ctx, "nomic-embed-text", "v1")
	if len(stale) != 1 || stale[0].Title != "Dune" || stale[0].Chunks != 1 {
		t.Fatalf("stale %+v", stale)
	}
	sum, err = ix.Run(ctx, id)
	if err != nil || sum.IndexedBooks != 1 || sum.StaleChunks != 0 {
		t.Fatalf("%+v %v", sum, err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_embeddings WHERE book_id = $1::uuid`, id).Scan(&rows)
	if rows != 1 {
		t.Fatalf("dune rows %d", rows)
	}

	// Unknown --book id indexes nothing.
	if sum, err := ix.Run(ctx, "00000000-0000-0000-0000-000000000000"); err != nil || sum.IndexedBooks != 0 {
		t.Fatalf("%+v %v", sum, err)
	}
}

func TestVectorLiteral(t *testing.T) {
	got := repository.VectorLiteral([]float32{0.5, -1, 0.25})
	if got != "[0.5,-1,0.25]" || !strings.HasPrefix(got, "[") {
		t.Fatal(got)
	}
}

var _ = model.ChunkRow{}
