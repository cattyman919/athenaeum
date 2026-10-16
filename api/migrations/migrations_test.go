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
	if tables != 16 { // docs/02 §1: 15 original tables + loans (M17) = 16 (+1 view)
		t.Fatalf("want 16 tables, got %d", tables)
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

func loansState(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (table, openIdx, userIdx, copyIdx bool) {
	t.Helper()
	if err := pool.QueryRow(ctx, `SELECT
		  EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='loans' AND table_schema='public'),
		  EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='loans_open_copy_key'),
		  EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='loans_user_open_idx'),
		  EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='loans_copy_idx')`).
		Scan(&table, &openIdx, &userIdx, &copyIdx); err != nil {
		t.Fatal(err)
	}
	return
}

func dueatState(ctx context.Context, t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name='book_copies' AND column_name='due_at')`).Scan(&exists)
	return exists
}

func TestMigration0007Loans(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)

	// After up: loans table and all indexes exist; book_copies.due_at is gone.
	tbl, oIdx, uIdx, cIdx := loansState(ctx, t, pool)
	if !tbl || !oIdx || !uIdx || !cIdx {
		t.Fatalf("after up: table=%v openIdx=%v userIdx=%v copyIdx=%v", tbl, oIdx, uIdx, cIdx)
	}
	if dueatState(ctx, t, pool) {
		t.Fatal("book_copies.due_at must be dropped by migration 0007")
	}

	// Verify partial unique index prevents two open loans for the same copy.
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"bd", "fl", "se", "ai", "sh", "bk", "u1", "u2"} {
		ids[k] = uuid.New()
	}
	setup := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO buildings (id,code,name) VALUES ($1,'MAIN','M')`, []any{ids["bd"]}},
		{`INSERT INTO floors (id,building_id,floor_number,name) VALUES ($1,$2,1,'F1')`, []any{ids["fl"], ids["bd"]}},
		{`INSERT INTO sections (id,floor_id,code,name,color) VALUES ($1,$2,'SF','SF','#000')`, []any{ids["se"], ids["fl"]}},
		{`INSERT INTO aisles (id,section_id,code) VALUES ($1,$2,'A-1')`, []any{ids["ai"], ids["se"]}},
		{`INSERT INTO shelves (id,aisle_id,code,row_level,height_tier) VALUES ($1,$2,'S-1',1,'Eye Level')`, []any{ids["sh"], ids["ai"]}},
		{`INSERT INTO books (id,title,section_id) VALUES ($1,'Dune',$2)`, []any{ids["bk"], ids["se"]}},
	}
	for _, s := range setup {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	copyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO book_copies (id,book_id,barcode,status,shelf_id) VALUES ($1,$2,'ATH-000001-C01','checked_out',$3)`, copyID, ids["bk"], ids["sh"]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,email,name) VALUES ($1,'a@x.com','A'),($2,'b@x.com','B')`, ids["u1"], ids["u2"]); err != nil {
		t.Fatal(err)
	}
	// First open loan — should succeed.
	loanID := uuid.New()
	dueAt := "2026-10-30 09:00:00+00"
	if _, err := pool.Exec(ctx,
		`INSERT INTO loans (id,copy_id,user_id,borrowed_at,due_at) VALUES ($1,$2,$3,now(),$4)`,
		loanID, copyID, ids["u1"], dueAt); err != nil {
		t.Fatalf("first loan: %v", err)
	}
	// Second open loan for same copy — must fail (partial unique index).
	if _, err := pool.Exec(ctx,
		`INSERT INTO loans (id,copy_id,user_id,borrowed_at,due_at) VALUES ($1,$2,$3,now(),$4)`,
		uuid.New(), copyID, ids["u2"], dueAt); err == nil {
		t.Fatal("two open loans for same copy must violate loans_open_copy_key")
	}
	// After closing the first loan a second open loan is allowed.
	if _, err := pool.Exec(ctx, `UPDATE loans SET returned_at=now() WHERE id=$1`, loanID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO loans (id,copy_id,user_id,borrowed_at,due_at) VALUES ($1,$2,$3,now(),$4)`,
		uuid.New(), copyID, ids["u2"], dueAt); err != nil {
		t.Fatalf("open loan after close: %v", err)
	}

	// Down: loans table dropped; due_at restored.
	execFile(ctx, t, pool, "0007_loans.down.sql")
	tbl, oIdx, uIdx, cIdx = loansState(ctx, t, pool)
	if tbl || oIdx || uIdx || cIdx {
		t.Fatalf("after down: table=%v openIdx=%v userIdx=%v copyIdx=%v", tbl, oIdx, uIdx, cIdx)
	}
	if !dueatState(ctx, t, pool) {
		t.Fatal("book_copies.due_at must be restored by down migration")
	}

	// Re-up: loans table back, due_at gone again.
	execFile(ctx, t, pool, "0007_loans.up.sql")
	tbl, oIdx, uIdx, cIdx = loansState(ctx, t, pool)
	if !tbl || !oIdx || !uIdx || !cIdx {
		t.Fatalf("after re-up: table=%v openIdx=%v userIdx=%v copyIdx=%v", tbl, oIdx, uIdx, cIdx)
	}
	if dueatState(ctx, t, pool) {
		t.Fatal("book_copies.due_at must be gone after re-up")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
