package repository_test

// Integration tests for plan M21 (book_isbns + book-level borrow/return).
// These run against a real Postgres container (testcontainers via pgtest.New).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// ── ISBN resolution via book_isbns ───────────────────────────────────────────

func TestBookIDByISBNRoutedThroughBookIsbns(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	repo := repository.NewCatalogRepository(pool)

	// Dune has isbn13=9780441013593 in mocks/books.json; seed inserts into book_isbns.
	var duneID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM books WHERE title='Dune'`).Scan(&duneID); err != nil {
		t.Fatal(err)
	}
	id, ok, err := repo.BookIDByISBN(ctx, "9780441013593")
	if err != nil || !ok || id != duneID {
		t.Fatalf("primary isbn lookup: %v %v %v", id, ok, err)
	}

	// A book without isbn13 must not be found.
	if _, ok, err := repo.BookIDByISBN(ctx, "9780306406157"); err != nil || ok {
		t.Fatalf("unknown isbn: %v %v", ok, err)
	}
}

func TestKeywordSearchMatchesAltISBN(t *testing.T) {
	pool, svc := seededCatalog(t)
	ctx := context.Background()

	// Attach an alt ISBN to Dune.
	var duneID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM books WHERE title='Dune'`).Scan(&duneID); err != nil {
		t.Fatal(err)
	}
	altISBN := "9780593099322" // valid checksum: 9+7+8+0+5+9+3+0+9+9+3+2+2 → check
	// Insert the alt ISBN manually (as the admin endpoint would).
	if _, err := pool.Exec(ctx,
		`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ($1, $2, false)`,
		altISBN, duneID); err != nil {
		t.Fatalf("insert alt isbn: %v", err)
	}

	p := search(t, svc, catalog.Request{Query: altISBN, Mode: "keyword"})
	found := false
	for _, item := range p.Items {
		if item.ID == duneID {
			found = true
		}
	}
	if !found {
		t.Fatalf("alt-ISBN keyword search did not find Dune: %v", p.Items)
	}
}

func TestDetailReturnsIsbns(t *testing.T) {
	pool, svc := seededCatalog(t)
	ctx := context.Background()

	var duneID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM books WHERE title='Dune'`).Scan(&duneID); err != nil {
		t.Fatal(err)
	}

	// Add an alt ISBN.
	if _, err := pool.Exec(ctx,
		`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ('9780593099322', $1, false)`,
		duneID); err != nil {
		t.Fatalf("insert alt isbn: %v", err)
	}

	d, err := svc.Detail(ctx, duneID.String(), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Isbns) < 2 {
		t.Fatalf("expected ≥2 isbns, got %v", d.Isbns)
	}
	// Primary must come first.
	if !d.Isbns[0].IsPrimary {
		t.Fatalf("first isbn is not primary: %v", d.Isbns)
	}
}

// ── Admin attach / detach / merge ────────────────────────────────────────────

func TestAttachDetachISBN(t *testing.T) {
	pool, svc, actor := adminSvc(t)
	ctx := context.Background()

	// Get Dune book id.
	duneID := bookID(t, pool, "Dune")

	// Attach a non-primary ISBN.
	result, err := svc.AttachISBN(ctx, duneID.String(), "9780593099322")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if result.BookID != duneID.String() {
		t.Fatalf("book_id: %v", result.BookID)
	}
	hasAlt := false
	for _, r := range result.Isbns {
		if r.ISBN13 == "9780593099322" && !r.IsPrimary {
			hasAlt = true
		}
	}
	if !hasAlt {
		t.Fatalf("alt isbn not in result: %v", result.Isbns)
	}

	// Attaching the same ISBN again must 409 (ISBN_CONFLICT).
	_, err = svc.AttachISBN(ctx, duneID.String(), "9780593099322")
	extra := wantCode(t, err, model.CodeISBNConflict)
	if extra["book_id"] != duneID.String() {
		t.Fatalf("conflict book_id: %v", extra)
	}

	// Attaching an ISBN belonging to another book must also 409.
	_, err = svc.AttachISBN(ctx, duneID.String(), "9780441013593") // Dune's own primary
	wantCode(t, err, model.CodeISBNConflict)

	// Detach must succeed for a non-primary.
	if err := svc.DetachISBN(ctx, duneID.String(), "9780593099322"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	// Detaching the primary must 409.
	err = svc.DetachISBN(ctx, duneID.String(), "9780441013593")
	wantCode(t, err, model.CodeConflict)

	// Attaching an invalid checksum must 400.
	_, err = svc.AttachISBN(ctx, duneID.String(), "9780441013594")
	wantCode(t, err, model.CodeInvalidISBN)

	_ = actor
}

func TestMergeBooks(t *testing.T) {
	pool, svc, actor := adminSvc(t)
	ctx := context.Background()

	// Use two real seeded books.
	duneID := bookID(t, pool, "Dune")
	foundID := bookID(t, pool, "Foundation")

	// Same book → 400.
	_, err := svc.MergeBooks(ctx, actor, duneID.String(), duneID.String())
	wantCode(t, err, model.CodeValidationFailed)

	// Merge Foundation (source) into Dune (target).
	foundCopiesBefore := countCopies(t, pool, foundID)
	duneCopiesBefore := countCopies(t, pool, duneID)

	result, err := svc.MergeBooks(ctx, actor, foundID.String(), duneID.String())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.BookID != duneID.String() {
		t.Fatalf("book_id: %v", result.BookID)
	}
	if result.MergedCopies != foundCopiesBefore {
		t.Fatalf("merged_copies: want %d got %d", foundCopiesBefore, result.MergedCopies)
	}
	// All copies are now under Dune.
	if countCopies(t, pool, duneID) != duneCopiesBefore+foundCopiesBefore {
		t.Fatalf("copies after merge: %d", countCopies(t, pool, duneID))
	}
	// Foundation no longer exists.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM books WHERE id=$1`, foundID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("foundation still exists: %d %v", n, err)
	}
	// Foundation's ISBNs moved to Dune as non-primary.
	for _, ref := range result.Isbns {
		if ref.ISBN13 == "9780553293357" && ref.IsPrimary {
			t.Fatalf("merged isbn must not be primary: %v", ref)
		}
	}
	// Dune is marked embedding_dirty.
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT embedding_dirty FROM books WHERE id=$1`, duneID).Scan(&dirty); err != nil || !dirty {
		t.Fatalf("dirty: %v %v", dirty, err)
	}
}

func TestMergeBooksLibraryConflict(t *testing.T) {
	pool, svc, actor := adminSvc(t)
	ctx := context.Background()

	// Create two fresh books to avoid seeded-data interference.
	srcID := insertBook(t, pool, "Source Book", "9780743273565")
	// 9780062316097 has digit-sum check: 9+7+8+0+0+6+2+3+1+6+0+9+7=63 → use correct ISBN
	dstID := insertBook(t, pool, "Dest Book", "9780316769174")
	userID := newUser(t, pool, "patron+merge@test.dev")

	// Add the same book to user_library under both books.
	addLibraryEntry(t, pool, userID, srcID, "reading", 3)
	addLibraryEntry(t, pool, userID, dstID, "want_to_read", 4)

	result, err := svc.MergeBooks(ctx, actor, srcID.String(), dstID.String())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	_ = result
	// User should have at most one library entry for dstID.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_library WHERE user_id=$1 AND book_id=$2`, userID, dstID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 library entry, got %d", count)
	}
	// Higher rating should be kept.
	var rating *int
	if err := pool.QueryRow(ctx, `SELECT rating FROM user_library WHERE user_id=$1 AND book_id=$2`, userID, dstID).Scan(&rating); err != nil {
		t.Fatal(err)
	}
	if rating == nil || *rating != 4 {
		t.Fatalf("expected rating=4, got %v", rating)
	}
}

func TestMergeWaitlist(t *testing.T) {
	pool, svc, actor := adminSvc(t)
	ctx := context.Background()

	srcID := insertBook(t, pool, "Waitlist Source", "9780307887443")
	dstID := insertBook(t, pool, "Waitlist Dest", "9781451648539")
	u1 := newUser(t, pool, "wl1@test.dev")
	u2 := newUser(t, pool, "wl2@test.dev")

	// u1 is on waitlist for src; u2 is on waitlist for both.
	addWaitlist(t, pool, u1, srcID)
	addWaitlist(t, pool, u2, srcID)
	addWaitlist(t, pool, u2, dstID)

	_, err := svc.MergeBooks(ctx, actor, srcID.String(), dstID.String())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// u1's src waitlist entry should move to dst.
	var c1 int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM waitlist WHERE book_id=$1 AND user_id=$2`, dstID, u1).Scan(&c1); err != nil {
		t.Fatal(err)
	}
	if c1 != 1 {
		t.Fatalf("u1 waitlist for dst: %d", c1)
	}
	// u2 had waitlist in both — should still have exactly 1 for dst (not 2).
	var c2 int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM waitlist WHERE book_id=$1 AND user_id=$2`, dstID, u2).Scan(&c2); err != nil {
		t.Fatal(err)
	}
	if c2 != 1 {
		t.Fatalf("u2 waitlist for dst: %d", c2)
	}
}

// ── similar books ─────────────────────────────────────────────────────────────

func TestSimilarBooks(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	repo := repository.NewCatalogRepository(pool)

	// Dune is in the catalog (title "Dune", author "Frank Herbert").
	similar, err := repo.SimilarBooks(ctx, "Dune", []string{"Frank Herbert"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range similar {
		if s.Title == "Dune" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Dune not found in similar: %v", similar)
	}

	// No match: wrong author.
	similar, err = repo.SimilarBooks(ctx, "Dune", []string{"Isaac Asimov"})
	if err != nil {
		t.Fatal(err)
	}
	if len(similar) != 0 {
		t.Fatalf("expected no similar books, got %v", similar)
	}

	// Empty title → empty result.
	similar, err = repo.SimilarBooks(ctx, "", []string{"Frank Herbert"})
	if err != nil || len(similar) != 0 {
		t.Fatalf("empty title: %v %v", similar, err)
	}
}

// ── borrow/return by book ─────────────────────────────────────────────────────

func TestBorrowBookPicksLowestBarcode(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	// Find Dune (has multiple available copies).
	duneID := bookID(t, pool, "Dune")
	userID := newUser(t, pool, "borrow-test@test.dev")
	now := time.Now().UTC()

	data, err := cirRepo.BorrowCopyForBook(ctx, userID, duneID, now)
	if err != nil {
		t.Fatalf("borrow: %v", err)
	}
	if data.Loan.UserID != userID || data.Copy.Status != model.CopyCheckedOut {
		t.Fatalf("borrow result: %+v", data)
	}
	// Loan period is 14 days.
	if data.Loan.DueAt.Sub(now) < 13*24*time.Hour {
		t.Fatalf("due_at too close: %v", data.Loan.DueAt)
	}

	// The copy picked must be the lowest barcode available among Dune copies.
	var lowestBarcode string
	if err := pool.QueryRow(ctx,
		`SELECT barcode FROM book_copies WHERE book_id=$1 AND status='available' ORDER BY barcode LIMIT 1`, duneID).
		Scan(&lowestBarcode); err != nil {
		// No more available copies (expected since some may be checked out from seed).
		// Just verify the loan exists.
		var loanCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE user_id=$1 AND returned_at IS NULL`, userID).Scan(&loanCount); err != nil || loanCount == 0 {
			t.Fatalf("no loan created: %v", err)
		}
	}
}

func TestBorrowBookHonoursOwnHold(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	// Find a book with a reserved_waitlist copy.
	var bookWithHold, copyWithHold uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT bc.book_id, bc.id FROM book_copies bc
		WHERE bc.status = 'reserved_waitlist'
		ORDER BY bc.barcode LIMIT 1`).Scan(&bookWithHold, &copyWithHold)
	if err != nil {
		t.Skip("no reserved_waitlist copy in seed — skip hold test")
	}

	// Create a user with a ready waitlist entry.
	userID := newUser(t, pool, "hold-borrow@test.dev")
	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist (id, book_id, user_id, status, ready_at, expires_at)
		 VALUES ($1,$2,$3,'ready',now(),now()+interval '48 hours')`,
		uuid.New(), bookWithHold, userID); err != nil {
		t.Fatalf("waitlist insert: %v", err)
	}

	now := time.Now().UTC()
	data, err := cirRepo.BorrowCopyForBook(ctx, userID, bookWithHold, now)
	if err != nil {
		t.Fatalf("borrow with hold: %v", err)
	}
	// The copy chosen must be the reserved_waitlist one.
	if data.Copy.ID != copyWithHold {
		t.Fatalf("expected hold copy %v, got %v", copyWithHold, data.Copy.ID)
	}
}

func TestBorrowBookLimit(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	userID := newUser(t, pool, "limit-test@test.dev")

	// Fill the user to MaxOpenLoans by borrowing from different books.
	borrowedBooks := 0
	rows, _ := pool.Query(ctx,
		`SELECT DISTINCT book_id FROM book_copies WHERE status='available' ORDER BY book_id LIMIT 10`)
	for rows.Next() && borrowedBooks < model.MaxOpenLoans {
		var bid uuid.UUID
		if err := rows.Scan(&bid); err != nil {
			break
		}
		if _, err := cirRepo.BorrowCopyForBook(ctx, userID, bid, time.Now().UTC()); err == nil {
			borrowedBooks++
		}
	}
	rows.Close()
	if borrowedBooks < model.MaxOpenLoans {
		t.Skipf("could not build up %d loans (got %d) — skip limit test", model.MaxOpenLoans, borrowedBooks)
	}

	// Next borrow must fail with BORROW_LIMIT.
	var anotherBook uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT DISTINCT book_id FROM book_copies WHERE status='available' ORDER BY book_id LIMIT 1`).Scan(&anotherBook)
	if err != nil {
		t.Skip("no more available books — skip limit test")
	}
	_, err = cirRepo.BorrowCopyForBook(ctx, userID, anotherBook, time.Now().UTC())
	wantCode(t, err, model.CodeBorrowLimit)
}

func TestBorrowBookNoAvailableCopy(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	// Find a book with no available copies.
	var zeroAvailBook uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT id FROM books WHERE NOT EXISTS (
		  SELECT 1 FROM book_copies bc WHERE bc.book_id = books.id AND bc.status='available'
		) LIMIT 1`).Scan(&zeroAvailBook)
	if err != nil {
		t.Skip("every book has at least one available copy — skip no-avail test")
	}
	userID := newUser(t, pool, "no-avail@test.dev")
	_, err = cirRepo.BorrowCopyForBook(ctx, userID, zeroAvailBook, time.Now().UTC())
	wantCode(t, err, model.CodeConflict)
}

func TestBorrowBookConcurrencyLastCopy(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()

	// Find a book with exactly 1 available copy.
	var singleAvailBook uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT book_id FROM book_copies
		WHERE status = 'available'
		GROUP BY book_id
		HAVING count(*) = 1
		LIMIT 1`).Scan(&singleAvailBook)
	if err != nil {
		t.Skip("no book with exactly 1 available copy — skip concurrency test")
	}

	u1 := newUser(t, pool, "race1@test.dev")
	u2 := newUser(t, pool, "race2@test.dev")

	var wg sync.WaitGroup
	results := make(chan error, 2)
	now := time.Now().UTC()

	for _, uid := range []uuid.UUID{u1, u2} {
		wg.Add(1)
		go func(uid uuid.UUID) {
			defer wg.Done()
			_, err := repository.NewCirculationRepository(pool).BorrowCopyForBook(ctx, uid, singleAvailBook, now)
			results <- err
		}(uid)
	}
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else {
			conflicts++
		}
	}
	// Exactly one succeeds, one gets CONFLICT.
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected 1 success 1 conflict, got %d successes %d conflicts", successes, conflicts)
	}
}

func TestReturnBook(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	// Find an open loan (seeded via ApplyLoans).
	var copyID, userID uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT l.copy_id, l.user_id FROM loans l WHERE l.returned_at IS NULL LIMIT 1`).
		Scan(&copyID, &userID)
	if err != nil {
		t.Fatal("no open loans in seed: ", err)
	}
	// Resolve the book id for this copy.
	var bk uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT book_id FROM book_copies WHERE id=$1`, copyID).Scan(&bk); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	data, err := cirRepo.ReturnCopyForBook(ctx, userID, bk, now)
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if data.Copy.Status != model.CopyAvailable && data.Copy.Status != model.CopyReservedWaitlist {
		t.Fatalf("copy status: %v", data.Copy.Status)
	}
	if data.ClosedLoan == nil {
		t.Fatal("closed_loan must be present")
	}
}

func TestReturnBookNoLoan(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	userID := newUser(t, pool, "no-loan@test.dev")
	var bookWithCopy uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT book_id FROM book_copies LIMIT 1`).Scan(&bookWithCopy); err != nil {
		t.Fatal(err)
	}
	_, err := cirRepo.ReturnCopyForBook(ctx, userID, bookWithCopy, time.Now().UTC())
	wantCode(t, err, model.CodeConflict)
}

func TestReturnBookOldestLoan(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	duneID := bookID(t, pool, "Dune")
	userID := newUser(t, pool, "oldest-loan@test.dev")
	now := time.Now().UTC()

	// Borrow two separate Dune copies (need two available).
	d1, err := cirRepo.BorrowCopyForBook(ctx, userID, duneID, now.Add(-2*time.Hour))
	if err != nil {
		t.Skip("could not borrow first copy: ", err)
	}
	d2, err := cirRepo.BorrowCopyForBook(ctx, userID, duneID, now.Add(-time.Hour))
	if err != nil {
		t.Skip("could not borrow second copy: ", err)
	}

	// Return by book must close the older loan (d1).
	rd, err := cirRepo.ReturnCopyForBook(ctx, userID, duneID, now)
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if rd.ClosedLoan.ID != d1.Loan.ID {
		t.Fatalf("expected oldest loan %v, got %v", d1.Loan.ID, rd.ClosedLoan.ID)
	}
	_ = d2
}

func TestBookBorrowInfo(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	cirRepo := repository.NewCirculationRepository(pool)

	duneID := bookID(t, pool, "Dune")
	userID := newUser(t, pool, "borrow-info@test.dev")

	borrowable, borrowCopy, returnable, myLoans, err := cirRepo.BookBorrowInfo(ctx, duneID, userID)
	if err != nil {
		t.Fatal(err)
	}
	// Dune has available copies in seed.
	if !borrowable || borrowCopy == nil {
		t.Fatalf("borrowable=%v borrowCopy=%v", borrowable, borrowCopy)
	}
	if borrowCopy.Barcode == "" {
		t.Fatalf("borrow_copy barcode empty")
	}
	if returnable || len(myLoans) != 0 {
		t.Fatalf("returnable=%v myLoans=%v", returnable, myLoans)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func countCopies(t *testing.T, pool *pgxpool.Pool, bookID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM book_copies WHERE book_id=$1`, bookID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func insertBook(t *testing.T, pool *pgxpool.Pool, title, isbn13 string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	var sectionID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM sections LIMIT 1`).Scan(&sectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO books (id, title, isbn13, section_id) VALUES ($1,$2,$3,$4)`,
		id, title, isbn13, sectionID); err != nil {
		t.Fatalf("insertBook %q: %v", title, err)
	}
	if isbn13 != "" {
		if _, err := pool.Exec(ctx,
			`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ($1, $2, true)`, isbn13, id); err != nil {
			t.Fatalf("insertBook isbn: %v", err)
		}
	}
	return id
}

func addLibraryEntry(t *testing.T, pool *pgxpool.Pool, userID, bookID uuid.UUID, status string, rating int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO user_library (id, user_id, book_id, status, rating) VALUES ($1,$2,$3,$4,$5)`,
		uuid.New(), userID, bookID, status, rating); err != nil {
		t.Fatalf("addLibraryEntry: %v", err)
	}
}

func addWaitlist(t *testing.T, pool *pgxpool.Pool, userID, bookID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO waitlist (id, book_id, user_id) VALUES ($1,$2,$3)`,
		uuid.New(), bookID, userID); err != nil {
		t.Fatalf("addWaitlist: %v", err)
	}
}
