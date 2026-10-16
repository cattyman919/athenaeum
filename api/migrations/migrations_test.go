package migrations_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
	"github.com/cattyman919/athenaeum/api/migrations"
)

func TestUpDownUp(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)

	var tables int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema='public' AND table_type='BASE TABLE'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 15 { // docs/02 §1: 15 tables (+1 view)
		t.Fatalf("want 15 tables, got %d", tables)
	}
	var view int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.views WHERE table_name='book_locations'`).Scan(&view); err != nil {
		t.Fatal(err)
	}
	if view != 1 {
		t.Fatal("book_locations view missing")
	}

	if err := pgtest.Migrate(ctx, pool, "down"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("down left %d tables", tables)
	}
	if err := pgtest.Migrate(ctx, pool, "up"); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}

func TestBookLocationsViewAndTriggers(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)

	ids := map[string]uuid.UUID{}
	for _, k := range []string{"bd", "fl", "se", "ai", "sh", "bk", "bc", "bc2"} {
		ids[k] = uuid.New()
	}
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO buildings (id, code, name) VALUES ($1,'MAIN','Main Building')`, []any{ids["bd"]}},
		{`INSERT INTO floors (id, building_id, floor_number, name) VALUES ($1,$2,3,'North Stacks')`, []any{ids["fl"], ids["bd"]}},
		{`INSERT INTO sections (id, floor_id, code, name, color) VALUES ($1,$2,'SF','Science Fiction','#6366f1')`, []any{ids["se"], ids["fl"]}},
		{`INSERT INTO aisles (id, section_id, code) VALUES ($1,$2,'A-1')`, []any{ids["ai"], ids["se"]}},
		{`INSERT INTO shelves (id, aisle_id, code, row_level, height_tier) VALUES ($1,$2,'S-2',4,'Eye Level')`, []any{ids["sh"], ids["ai"]}},
		{`INSERT INTO books (id, title, section_id) VALUES ($1,'Dune',$2)`, []any{ids["bk"], ids["se"]}},
		{`INSERT INTO book_copies (id, book_id, barcode, shelf_id) VALUES ($1,$2,'ATH-002941-C01',$3)`, []any{ids["bc"], ids["bk"], ids["sh"]}},
		{`INSERT INTO book_copies (id, book_id, barcode) VALUES ($1,$2,'ATH-002941-C02')`, []any{ids["bc2"], ids["bk"]}},
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}

	var path, ladder string
	if err := pool.QueryRow(ctx,
		`SELECT path, display_ladder FROM book_locations WHERE copy_id=$1`, ids["bc"]).Scan(&path, &ladder); err != nil {
		t.Fatal(err)
	}
	if path != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("path %q", path)
	}
	if want := "Main Building (North Stacks) → Science Fiction → A-1 → S-2 (Eye Level)"; ladder != want {
		t.Fatalf("ladder %q want %q", ladder, want)
	}
	var nullPath *string
	if err := pool.QueryRow(ctx, `SELECT path FROM book_locations WHERE copy_id=$1`, ids["bc2"]).Scan(&nullPath); err != nil {
		t.Fatal(err)
	}
	if nullPath != nil {
		t.Fatalf("shelf-less copy should have NULL path, got %q", *nullPath)
	}

	// Barcode CHECK constraint.
	if _, err := pool.Exec(ctx, `INSERT INTO book_copies (id, book_id, barcode) VALUES ($1,$2,'BAD')`, uuid.New(), ids["bk"]); err == nil {
		t.Fatal("bad barcode must violate CHECK")
	}

	// updated_at trigger.
	var advanced bool
	if err := pool.QueryRow(ctx,
		`WITH before AS (SELECT updated_at AS t FROM books WHERE id=$1),
		      _upd AS (UPDATE books SET title='Dune Messiah' WHERE id=$1 RETURNING updated_at)
		 SELECT (SELECT updated_at FROM _upd) >= (SELECT t FROM before)`, ids["bk"]).Scan(&advanced); err != nil {
		t.Fatal(err)
	}
	if !advanced {
		t.Fatal("updated_at not advanced")
	}
	var changed bool
	if _, err := pool.Exec(ctx, `UPDATE books SET updated_at='2000-01-01' WHERE id=$1`, ids["bk"]); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT updated_at > '2000-01-02' FROM books WHERE id=$1`, ids["bk"]).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("trigger should override manual updated_at")
	}

	// Vector column accepts 768 dims and rejects others.
	vec := "[" + repeat("0.1,", 767) + "0.1]"
	if _, err := pool.Exec(ctx,
		`INSERT INTO book_embeddings (book_id, chunk_index, content, embedding, model, chunker_version)
		 VALUES ($1,0,'x',$2::vector,'nomic-embed-text','v1')`, ids["bk"], vec); err != nil {
		t.Fatalf("768-dim insert: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO book_embeddings (book_id, chunk_index, content, embedding, model, chunker_version)
		 VALUES ($1,1,'x','[0.1,0.2]'::vector,'m','v1')`, ids["bk"]); err == nil {
		t.Fatal("wrong dimension must fail")
	}
}

func embeddingDirtyState(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (column, index bool) {
	t.Helper()
	if err := pool.QueryRow(ctx, `SELECT
		  EXISTS (SELECT 1 FROM information_schema.columns
		          WHERE table_name='books' AND column_name='embedding_dirty'
		            AND data_type='boolean' AND is_nullable='NO' AND column_default='false'),
		  EXISTS (SELECT 1 FROM pg_indexes
		          WHERE indexname='books_embedding_dirty_idx' AND indexdef LIKE '%WHERE embedding_dirty%')`).
		Scan(&column, &index); err != nil {
		t.Fatal(err)
	}
	return column, index
}

func execFile(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	b, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(b)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestMigration0006EmbeddingDirty(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)

	if col, idx := embeddingDirtyState(ctx, t, pool); !col || !idx {
		t.Fatalf("after up: column=%v index=%v", col, idx)
	}
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO books (id, title) VALUES ($1,'Dune')`, id); err != nil {
		t.Fatal(err)
	}
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT embedding_dirty FROM books WHERE id=$1`, id).Scan(&dirty); err != nil || dirty {
		t.Fatalf("default must be false: %v %v", dirty, err)
	}

	execFile(ctx, t, pool, "0006_books_embedding_dirty.down.sql")
	if col, idx := embeddingDirtyState(ctx, t, pool); col || idx {
		t.Fatalf("after down: column=%v index=%v", col, idx)
	}
	execFile(ctx, t, pool, "0006_books_embedding_dirty.up.sql")
	if col, idx := embeddingDirtyState(ctx, t, pool); !col || !idx {
		t.Fatalf("after re-up: column=%v index=%v", col, idx)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
