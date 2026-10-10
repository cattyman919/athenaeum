package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/building"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

type stubEnricher struct{ err error }

func (s stubEnricher) Lookup(context.Context, string) (olbooks.Metadata, error) {
	if s.err != nil {
		return olbooks.Metadata{}, s.err
	}
	return olbooks.Metadata{Key: "/books/OL1M"}, nil
}

var seedUsers = []seed.UserSpec{
	{Email: "admin@athenaeum.dev", Name: "Library Admin", Password: "athenaeum-demo", Role: model.RoleAdmin},
	{Email: "demo@athenaeum.dev", Name: "Demo Patron", Password: "athenaeum-demo", Role: model.RoleUser},
}

func TestSeedParityIdempotencyAndReset(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	store := repository.NewSeedStore(pool)
	fx, err := seed.Load("../../mocks")
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.Validate(); err != nil {
		t.Fatal(err)
	}
	want := model.SeedCounts{Users: 2, Books: 40, Copies: 60, Shelves: 40, CheckedOut: 12, ZeroCopyBooks: 5, LibraryEntries: 24, WaitlistRows: 2}

	got, err := seed.Run(ctx, store, stubEnricher{}, fx, seed.Options{Users: seedUsers}, logging.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("counts %+v, want %+v", got, want)
	}

	// Idempotent: second run keeps identical counts and event rows.
	var events1, events2 int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM copy_events`).Scan(&events1)
	got, err = seed.Run(ctx, store, stubEnricher{err: olbooks.ErrUnavailable}, fx, seed.Options{Users: seedUsers}, logging.Discard())
	if err != nil || got != want {
		t.Fatalf("rerun: %+v %v", got, err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM copy_events`).Scan(&events2)
	if events1 != 60 || events2 != 60 {
		t.Fatalf("copy_events %d then %d, want 60/60", events1, events2)
	}

	// Barcodes and locations: Dune C01 is checked out on MAIN/3F/SF/A-1/S-2; 20% checked out.
	var status, path string
	if err := pool.QueryRow(ctx,
		`SELECT copy_status, path FROM book_locations WHERE barcode = 'ATH-002941-C01'`).Scan(&status, &path); err != nil {
		t.Fatal(err)
	}
	if status != "checked_out" || path != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("Dune C01: %s %s", status, path)
	}
	var withKey, availableBooks int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM books WHERE open_library_key = '/books/OL1M'`).Scan(&withKey)
	if withKey != 40 { // first (successful) run enriched; failed second run keeps it
		t.Fatalf("enriched keys %d", withKey)
	}
	_ = pool.QueryRow(ctx, `SELECT count(DISTINCT book_id) FROM book_copies WHERE status = 'available'`).Scan(&availableBooks)
	if availableBooks != 35 { // docs/13 §5: /catalog?availability=available → 35
		t.Fatalf("books with available copy: %d", availableBooks)
	}
	var tree string
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM floors)||'/'||(SELECT count(*) FROM sections)||'/'||(SELECT count(*) FROM aisles)`).Scan(&tree)
	if tree != "3/5/15" {
		t.Fatalf("tree %s", tree)
	}

	// ResolvePath against the DB (same resolver as admin APIs).
	repo := repository.NewBuildingRepository(pool)
	if _, err := building.ResolvePath(ctx, repo, "MAIN/3F/SF/A-1/S-2"); err != nil {
		t.Fatal(err)
	}
	var de *model.Error
	if _, err := building.ResolvePath(ctx, repo, "MAIN/3F/SF/A-9/S-2"); !errors.As(err, &de) || de.Code != model.CodeNotFound {
		t.Fatalf("missing aisle: %v", err)
	}

	// --reset wipes catalog content, keeps users, then reseeds identically.
	if _, err := pool.Exec(ctx, `INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at)
		SELECT gen_random_uuid(), id, 'h', now() + interval '1 day' FROM users LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if err := store.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	c, _ := store.Counts(ctx)
	if c.Books != 0 || c.Copies != 0 || c.Shelves != 0 || c.Users != 2 {
		t.Fatalf("after reset %+v", c)
	}
	var tokens int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens`).Scan(&tokens)
	if tokens != 1 {
		t.Fatal("refresh_tokens must survive reset")
	}
	got, err = seed.Run(ctx, store, nil, fx, seed.Options{Users: seedUsers}, logging.Discard())
	if err != nil || got != want {
		t.Fatalf("reseed: %+v %v", got, err)
	}
}

func TestSeedFailureLeavesNoPartialBook(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	store := repository.NewSeedStore(pool)
	fx, _ := seed.Load("../../mocks")
	// Bypass Validate on purpose: DB-level resolution must still hard-fail and roll back.
	fx.Books = fx.Books[:1]
	fx.Books[0].Copies = append([]model.CopyFixture(nil), fx.Books[0].Copies...)
	fx.Books[0].Copies[1].ShelfPath = "MAIN/3F/SF/A-1/S-9"
	_, err := seed.Run(ctx, store, nil, fx, seed.Options{}, logging.Discard())
	if err == nil {
		t.Fatal("unknown shelf must abort the seed")
	}
	var books, copies int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM books`).Scan(&books)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_copies`).Scan(&copies)
	if books != 0 || copies != 0 {
		t.Fatalf("partial write: books=%d copies=%d", books, copies)
	}
}
