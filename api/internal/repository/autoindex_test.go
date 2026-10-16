package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

func dirty(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) bool {
	t.Helper()
	var d bool
	if err := pool.QueryRow(context.Background(), `SELECT embedding_dirty FROM books WHERE id = $1`, id).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

func pendingIDs(t *testing.T, st *repository.IndexStore) map[string]model.PendingBook {
	t.Helper()
	ps, err := st.PendingBooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]model.PendingBook{}
	for _, p := range ps {
		out[p.ID] = p
	}
	return out
}

type countingNotifier struct{ n int }

func (c *countingNotifier) Notify() { c.n++ }

func TestAdminWritesMarkEmbeddingDirty(t *testing.T) {
	ctx := context.Background()
	pool, svc, actor := adminSvc(t)
	notes := &countingNotifier{}
	svc.WithIndexNotifier(notes)
	st := repository.NewIndexStore(pool)

	if p := pendingIDs(t, st); len(p) != 0 {
		t.Fatalf("seeded + indexed catalog should have no backlog: %v", p)
	}

	b, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "Solaris"}})
	if err != nil {
		t.Fatal(err)
	}
	if !dirty(t, pool, b.ID) || notes.n != 1 {
		t.Fatalf("create must mark dirty and notify (notes=%d)", notes.n)
	}

	dune := bookID(t, pool, "Dune")
	year := model.Optional[int]{Set: true, Value: 1966}
	cover := model.Optional[string]{Set: true, Value: "https://covers.example/dune.jpg"}
	sec := model.Optional[string]{Set: true, Value: "SF"}
	if _, err := svc.PatchBook(ctx, dune.String(), admin.BookPatchRequest{
		Patch: model.BookPatch{PublishedYear: year, CoverURL: cover}, Section: sec,
	}); err != nil {
		t.Fatal(err)
	}
	if dirty(t, pool, dune) || notes.n != 1 {
		t.Fatalf("non-RAG patch must not mark dirty or notify (notes=%d)", notes.n)
	}

	for name, p := range map[string]model.BookPatch{
		"title":       {Title: str("Dune (Deluxe)")},
		"authors":     {Authors: &[]string{"Frank Herbert", "Brian Herbert"}},
		"description": {Description: model.Optional[string]{Set: true, Value: "A desert planet."}},
	} {
		if _, err := pool.Exec(ctx, `UPDATE books SET embedding_dirty = false WHERE id = $1`, dune); err != nil {
			t.Fatal(err)
		}
		before := notes.n
		if _, err := svc.PatchBook(ctx, dune.String(), admin.BookPatchRequest{Patch: p}); err != nil {
			t.Fatal(err)
		}
		if !dirty(t, pool, dune) || notes.n != before+1 {
			t.Fatalf("%s patch must mark dirty and notify", name)
		}
	}

	p := pendingIDs(t, st)
	if len(p) != 2 || !p[b.ID.String()].Dirty || !p[dune.String()].Dirty {
		t.Fatalf("pending %v", p)
	}
}

func TestIndexStorePendingAndClearDirty(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fx, _ := seed.Load("../../mocks")
	if _, err := seed.Run(ctx, repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}
	st := repository.NewIndexStore(pool)

	// Fresh seed: dirty=false everywhere, but every book lacks embeddings → backlog.
	var flagged int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM books WHERE embedding_dirty`).Scan(&flagged)
	if flagged != 0 {
		t.Fatalf("seed must leave embedding_dirty=false, %d flagged", flagged)
	}
	if p := pendingIDs(t, st); len(p) != 40 {
		t.Fatalf("fresh seed backlog %d, want 40", len(p))
	}

	// Blank-description book without embeddings is not pending unless flagged.
	blank := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO books (id, title, description) VALUES ($1, 'Blank', '  ')`, blank); err != nil {
		t.Fatal(err)
	}
	if _, ok := pendingIDs(t, st)[blank.String()]; ok {
		t.Fatal("blank unflagged book must not be pending")
	}

	ix := index.New(st, constEmb{}, "nomic-embed-text", logging.Discard())
	if _, err := ix.Run(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if p := pendingIDs(t, st); len(p) != 0 {
		t.Fatalf("indexed catalog backlog %v", p)
	}

	// Concurrent edit guard: a changed updated_at keeps the flag.
	dune := bookID(t, pool, "Dune")
	if _, err := pool.Exec(ctx, `UPDATE books SET embedding_dirty = true WHERE id = $1`, dune); err != nil {
		t.Fatal(err)
	}
	snap := pendingIDs(t, st)[dune.String()]
	if !snap.Dirty {
		t.Fatalf("dune pending %+v", snap)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := pool.Exec(ctx, `UPDATE books SET title = 'Dune!' WHERE id = $1`, dune); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.ClearDirty(ctx, dune.String(), snap.UpdatedAt); err != nil || ok || !dirty(t, pool, dune) {
		t.Fatalf("stale snapshot must not clear: ok=%v err=%v", ok, err)
	}
	snap = pendingIDs(t, st)[dune.String()]
	if ok, err := st.ClearDirty(ctx, dune.String(), snap.UpdatedAt); err != nil || !ok || dirty(t, pool, dune) {
		t.Fatalf("fresh snapshot must clear: ok=%v err=%v", ok, err)
	}

	// A book deleted between read and write is skipped, not an FK failure.
	gone := uuid.NewString()
	rows := []model.ChunkRow{{BookID: gone, Content: "x", Embedding: make([]float32, 768), Model: "m", ChunkerVersion: "v1"}}
	if err := st.Replace(ctx, []string{gone}, rows); err != nil {
		t.Fatalf("replace for deleted book: %v", err)
	}
}

func TestWorkerEmbedsAdminCreatedBook(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, svc, actor := adminSvc(t)
	st := repository.NewIndexStore(pool)
	w := index.NewWorker(st, index.New(st, constEmb{}, "nomic-embed-text", logging.Discard()), logging.Discard()).
		WithTimings(time.Hour, time.Millisecond, 10*time.Millisecond)
	svc.WithIndexNotifier(w)
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	b, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{
		Title: "Solaris", Description: str("A planet-wide ocean resists every attempt at contact."),
	}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for dirty(t, pool, b.ID) {
		if time.Now().After(deadline) {
			t.Fatal("worker did not embed the new book")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_embeddings WHERE book_id = $1`, b.ID).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rows %d", rows)
	}
	cancel()
	<-done
}

func TestIndexStoreStatusMatchesPending(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fx, _ := seed.Load("../../mocks")
	if _, err := seed.Run(ctx, repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}
	st := repository.NewIndexStore(pool)
	dune := bookID(t, pool, "Dune")
	if _, err := pool.Exec(ctx, `UPDATE books SET embedding_dirty = true WHERE id = $1`, dune); err != nil {
		t.Fatal(err)
	}

	want, err := st.PendingBooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.PendingCount(ctx)
	if err != nil || n != len(want) || n != 40 {
		t.Fatalf("count %d err %v, want %d", n, err, len(want))
	}
	s, err := st.PendingStatus(ctx, 5)
	if err != nil || s.PendingCount != 40 || len(s.PendingBookIDs) != 5 {
		t.Fatalf("status %+v %v", s, err)
	}
	for i, id := range s.PendingBookIDs {
		if id != want[i].ID {
			t.Fatalf("ids[%d] = %s, want worker order %s", i, id, want[i].ID)
		}
	}
	if s.PendingBookIDs[0] != dune.String() {
		t.Fatalf("dirty book must come first: %v", s.PendingBookIDs)
	}

	if _, err := index.New(st, constEmb{}, "nomic-embed-text", logging.Discard()).Run(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE books SET embedding_dirty = false`); err != nil {
		t.Fatal(err)
	}
	s, err = st.PendingStatus(ctx, 5)
	if err != nil || s.PendingCount != 0 || s.PendingBookIDs == nil || len(s.PendingBookIDs) != 0 {
		t.Fatalf("drained status %+v %v", s, err)
	}
	if n, _ := st.PendingCount(ctx); n != 0 {
		t.Fatalf("drained count %d", n)
	}
}
