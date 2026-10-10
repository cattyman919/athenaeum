package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
)

func newUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1,$2,$3)`, id, email, email); err != nil {
		t.Fatal(err)
	}
	return id
}

func bookID(t *testing.T, pool *pgxpool.Pool, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM books WHERE title = $1`, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func str(s string) *string { return &s }

func TestLibraryRepository(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewLibraryRepository(pool)
	u := newUser(t, pool, "lib@athenaeum.dev")
	dune, neuro, hyp := bookID(t, pool, "Dune"), bookID(t, pool, "Neuromancer"), bookID(t, pool, "Hyperion")

	exists, avail, err := repo.BookAvailability(ctx, dune)
	if err != nil || !exists || avail != 2 {
		t.Fatalf("availability %v %v %v", exists, avail, err)
	}
	if exists, _, _ := repo.BookAvailability(ctx, uuid.New()); exists {
		t.Fatal("unknown book must not exist")
	}

	// Create, then idempotent re-add.
	e, created, err := repo.Upsert(ctx, u, dune, model.LibraryPatch{Status: str("reading")}, "want_to_read")
	if err != nil || !created || e.Status != "reading" || e.IsFavorite {
		t.Fatalf("create %+v %v %v", e, created, err)
	}
	if _, created, _ := repo.Upsert(ctx, u, dune, model.LibraryPatch{Status: str("reading")}, "want_to_read"); created {
		t.Fatal("second upsert must not create")
	}
	if has, _ := repo.HasEntry(ctx, u, dune); !has {
		t.Fatal("HasEntry")
	}

	// Partial patch keeps untouched fields; null clears.
	fav := true
	e, _, err = repo.Upsert(ctx, u, dune, model.LibraryPatch{
		IsFavorite: &fav, ProgressPct: model.Optional[int]{Set: true, Value: 40}, Rating: model.Optional[int]{Set: true, Value: 5},
	}, "want_to_read")
	if err != nil || e.Status != "reading" || !e.IsFavorite || e.ProgressPct == nil || *e.ProgressPct != 40 || *e.Rating != 5 {
		t.Fatalf("patch %+v %v", e, err)
	}
	e, _, _ = repo.Upsert(ctx, u, dune, model.LibraryPatch{Rating: model.Optional[int]{Set: true, Null: true}}, "want_to_read")
	if e.Rating != nil || *e.ProgressPct != 40 {
		t.Fatalf("null clears only that field: %+v", e)
	}

	// PATCH on a missing row upserts with the default stage.
	e, created, _ = repo.Upsert(ctx, u, neuro, model.LibraryPatch{IsFavorite: &fav}, "want_to_read")
	if !created || e.Status != "want_to_read" || !e.IsFavorite {
		t.Fatalf("upsert default %+v", e)
	}
	_, _, _ = repo.Upsert(ctx, u, hyp, model.LibraryPatch{Status: str("read")}, "want_to_read")

	// Counts + filters.
	c, err := repo.Counts(ctx, u)
	if err != nil || c != (model.LibraryCounts{Total: 3, Reading: 1, Read: 1, WantToRead: 1, Favorites: 2}) {
		t.Fatalf("counts %+v %v", c, err)
	}
	items, total, err := repo.List(ctx, u, model.LibraryFilter{}, 1, 20)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("all %d %v", total, err)
	}
	_, total, _ = repo.List(ctx, u, model.LibraryFilter{Statuses: []string{"reading"}}, 1, 20)
	if total != 1 {
		t.Fatalf("reading %d", total)
	}
	_, total, _ = repo.List(ctx, u, model.LibraryFilter{Favorites: true}, 1, 20)
	if total != 2 {
		t.Fatalf("favorites %d", total)
	}
	_, total, _ = repo.List(ctx, u, model.LibraryFilter{Statuses: []string{"read"}, Favorites: true}, 1, 20)
	if total != 3 { // union: read ∪ favorites
		t.Fatalf("union %d", total)
	}
	page2, total, _ := repo.List(ctx, u, model.LibraryFilter{}, 2, 2)
	if total != 3 || len(page2) != 1 {
		t.Fatalf("paging %d/%d", len(page2), total)
	}
	var dune1 model.LibraryItem
	for _, it := range items {
		if it.Book.ID == dune {
			dune1 = it
		}
	}
	if dune1.Location == nil || dune1.Location.Path != "MAIN/3F/SF/A-1/S-2" || dune1.Location.Floor != 3 || dune1.Location.ShelfCode != "S-2" {
		t.Fatalf("location %+v", dune1.Location)
	}

	// Another user's library is isolated.
	other := newUser(t, pool, "other@athenaeum.dev")
	if c, _ := repo.Counts(ctx, other); c.Total != 0 {
		t.Fatalf("isolation %+v", c)
	}

	if err := repo.Delete(ctx, u, dune); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, u, dune); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
	if c, _ := repo.Counts(ctx, u); c.Total != 2 {
		t.Fatalf("after delete %+v", c)
	}
}

func TestWaitlistLifecycle(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewWaitlistRepository(pool)
	a, b := newUser(t, pool, "a@athenaeum.dev"), newUser(t, pool, "b@athenaeum.dev")
	oxford := bookID(t, pool, "The Oxford Guide to Library Research")

	ea, created, err := repo.Join(ctx, a, oxford)
	if err != nil || !created || ea.Position != 1 || ea.Status != "waiting" {
		t.Fatalf("a: %+v %v %v", ea, created, err)
	}
	again, created, _ := repo.Join(ctx, a, oxford)
	if created || again.ID != ea.ID || again.Position != 1 {
		t.Fatalf("idempotent join: %+v", again)
	}
	eb, _, _ := repo.Join(ctx, b, oxford)
	if eb.Position != 2 {
		t.Fatalf("b position %d", eb.Position)
	}
	items, err := repo.List(ctx, b)
	if err != nil || len(items) != 1 || items[0].Position != 2 || items[0].Book.Title != "The Oxford Guide to Library Research" || items[0].Kind != "hold" {
		t.Fatalf("list %+v %v", items, err)
	}

	// Leaving moves the queue up and frees a later re-join.
	if err := repo.Leave(ctx, a, oxford); err != nil {
		t.Fatal(err)
	}
	if err := repo.Leave(ctx, a, oxford); err != nil {
		t.Fatalf("leave idempotent: %v", err)
	}
	items, _ = repo.List(ctx, b)
	if items[0].Position != 1 {
		t.Fatalf("b should move up: %d", items[0].Position)
	}
	if rejoin, created, _ := repo.Join(ctx, a, oxford); !created || rejoin.Position != 2 {
		t.Fatalf("rejoin after cancel: %+v", rejoin)
	}
}

func TestWaitlistSweepPromotesThenReleases(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewWaitlistRepository(pool)
	a, b := newUser(t, pool, "a@athenaeum.dev"), newUser(t, pool, "b@athenaeum.dev")
	book := bookID(t, pool, "The Elements of Style (4th ed.)")

	// One copy held for the waitlist; A is ready (window already over), B waits.
	copyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO book_copies (id, book_id, barcode, status) VALUES ($1,$2,'ATH-009901-C01','reserved_waitlist')`, copyID, book); err != nil {
		t.Fatal(err)
	}
	ea, _, _ := repo.Join(ctx, a, book)
	if _, _, err := repo.Join(ctx, b, book); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE waitlist SET status='ready', ready_at=$2, expires_at=$2 WHERE id=$1`, ea.ID, past); err != nil {
		t.Fatal(err)
	}

	// Nothing expires early.
	if res, err := repo.Sweep(ctx, past.Add(-time.Hour)); err != nil || res.Expired != 0 {
		t.Fatalf("early sweep %+v %v", res, err)
	}

	now := time.Now()
	res, err := repo.Sweep(ctx, now)
	if err != nil || res != (model.SweepResult{Expired: 1, Promoted: 1}) {
		t.Fatalf("first sweep %+v %v", res, err)
	}
	var sa, sb string
	var expires time.Time
	_ = pool.QueryRow(ctx, `SELECT status FROM waitlist WHERE id=$1`, ea.ID).Scan(&sa)
	_ = pool.QueryRow(ctx, `SELECT status, expires_at FROM waitlist WHERE user_id=$1 AND book_id=$2`, b, book).Scan(&sb, &expires)
	if sa != "expired" || sb != "ready" || expires.Sub(now) < 47*time.Hour || expires.Sub(now) > 49*time.Hour {
		t.Fatalf("a=%s b=%s expires in %v", sa, sb, expires.Sub(now))
	}
	var copyStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM book_copies WHERE id=$1`, copyID).Scan(&copyStatus)
	if copyStatus != "reserved_waitlist" {
		t.Fatalf("copy stays reserved while someone is next: %s", copyStatus)
	}

	// B's window lapses with nobody waiting → the copy returns to the shelf.
	later := now.Add(49 * time.Hour)
	res, err = repo.Sweep(ctx, later)
	if err != nil || res != (model.SweepResult{Expired: 1, Released: 1}) {
		t.Fatalf("second sweep %+v %v", res, err)
	}
	_ = pool.QueryRow(ctx, `SELECT status FROM book_copies WHERE id=$1`, copyID).Scan(&copyStatus)
	var events int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM copy_events WHERE copy_id=$1 AND source='waitlist' AND to_status='available'`, copyID).Scan(&events)
	if copyStatus != "available" || events != 1 {
		t.Fatalf("copy %s events %d", copyStatus, events)
	}
	// Sweep is idempotent.
	if res, _ := repo.Sweep(ctx, later); res != (model.SweepResult{}) {
		t.Fatalf("third sweep %+v", res)
	}
}

func TestLeavingAReadyHoldReleasesTheCopy(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewWaitlistRepository(pool)
	a := newUser(t, pool, "a@athenaeum.dev")
	book := bookID(t, pool, "The Elements of Style (4th ed.)")
	copyID := uuid.New()
	_, _ = pool.Exec(ctx, `INSERT INTO book_copies (id, book_id, barcode, status) VALUES ($1,$2,'ATH-009902-C01','reserved_waitlist')`, copyID, book)
	ea, _, _ := repo.Join(ctx, a, book)
	_, _ = pool.Exec(ctx, `UPDATE waitlist SET status='ready', ready_at=now(), expires_at=now()+interval '48 hours' WHERE id=$1`, ea.ID)

	if err := repo.Leave(ctx, a, book); err != nil {
		t.Fatal(err)
	}
	var st string
	_ = pool.QueryRow(ctx, `SELECT status FROM book_copies WHERE id=$1`, copyID).Scan(&st)
	if st != "available" {
		t.Fatalf("copy should be released: %s", st)
	}
}
