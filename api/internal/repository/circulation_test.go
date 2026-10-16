package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
)

// seedOpenLoan inserts an open loan for a copy (test helper).
func seedOpenLoan(t *testing.T, pool *pgxpool.Pool, copyID, userID uuid.UUID, borrowedAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO loans (id,copy_id,user_id,borrowed_at,due_at) VALUES ($1,$2,$3,$4,$5)`,
		id, copyID, userID, borrowedAt, borrowedAt.Add(model.LoanPeriod)); err != nil {
		t.Fatalf("seedOpenLoan: %v", err)
	}
	return id
}

// ── Return ────────────────────────────────────────────────────────────────────

func TestReturnCopyToShelf(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	u := newUser(t, pool, "returner@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C01'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)

	// The seed (ApplyLoans) already created an open loan for this checked_out copy.
	// Retrieve its id for assertion after the return.
	var loanID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM loans WHERE copy_id=$1 AND returned_at IS NULL`, copyID).Scan(&loanID); err != nil {
		t.Fatalf("seeded loan not found: %v", err)
	}

	d, err := repo.ReturnCopy(ctx, u, copyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Copy.ID != copyID || d.Copy.Status != model.CopyAvailable || !d.Copy.LastStateChange.Equal(now) || d.Hold != nil {
		t.Fatalf("copy %+v hold %+v", d.Copy, d.Hold)
	}
	if d.Location == nil || d.Location.Path != "MAIN/3F/SF/A-1/S-2" || d.Location.SectionCode != "SF" {
		t.Fatalf("location %+v", d.Location)
	}
	if d.Total != 3 || d.Available != 3 || len(d.Statuses) != 1 || d.Statuses[0] != model.CopyAvailable {
		t.Fatalf("availability %d/%d %v", d.Available, d.Total, d.Statuses)
	}
	var from, to, source string
	var actor uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT from_status, to_status, source, actor_id FROM copy_events
		WHERE copy_id = $1 ORDER BY id DESC LIMIT 1`, copyID).Scan(&from, &to, &source, &actor); err != nil {
		t.Fatal(err)
	}
	if from != "checked_out" || to != "available" || source != "self_return" || actor != u {
		t.Fatalf("event %s→%s %s %v", from, to, source, actor)
	}
	// The loan must be closed.
	if d.ClosedLoan == nil || d.ClosedLoan.ID != loanID || !d.ClosedLoan.ReturnedAt.Equal(now) {
		t.Fatalf("closed_loan %+v", d.ClosedLoan)
	}
	var returnedAt time.Time
	_ = pool.QueryRow(ctx, `SELECT returned_at FROM loans WHERE id=$1`, loanID).Scan(&returnedAt)
	if !returnedAt.Equal(now) {
		t.Fatalf("loan.returned_at %v want %v", returnedAt, now)
	}

	// A second return conflicts and reports the current status; nothing is appended.
	_, err = repo.ReturnCopy(ctx, u, copyID, now)
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeConflict || de.Extra["current_status"] != "available" {
		t.Fatalf("second return: %v", err)
	}
	var events int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM copy_events WHERE copy_id = $1 AND source = 'self_return'`, copyID).Scan(&events)
	if events != 1 {
		t.Fatalf("self_return events %d", events)
	}

	if _, err := repo.ReturnCopy(ctx, u, uuid.New(), now); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown copy: %v", err)
	}
}

func TestReturnCopyPromotesOldestWaitingHold(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	returner := newUser(t, pool, "desk@athenaeum.dev")
	a, b := newUser(t, pool, "a@athenaeum.dev"), newUser(t, pool, "b@athenaeum.dev")
	book := bookID(t, pool, "Neuromancer")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `UPDATE book_copies SET status = 'checked_out'
		WHERE id = (SELECT id FROM book_copies WHERE book_id = $1 ORDER BY barcode LIMIT 1) RETURNING id`, book).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	var holdA, holdB uuid.UUID
	base := time.Now().Add(-time.Hour)
	if err := pool.QueryRow(ctx, `INSERT INTO waitlist (id, book_id, user_id, created_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		uuid.New(), book, b, base.Add(time.Minute)).Scan(&holdB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO waitlist (id, book_id, user_id, created_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		uuid.New(), book, a, base).Scan(&holdA); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	d, err := repo.ReturnCopy(ctx, returner, copyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Copy.Status != model.CopyReservedWaitlist {
		t.Fatalf("copy status %s", d.Copy.Status)
	}
	if d.Hold == nil || d.Hold.ID != holdA || d.Hold.Status != model.WaitReady || !d.Hold.ExpiresAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("hold %+v", d.Hold)
	}
	var sa, sb string
	var readyAt time.Time
	_ = pool.QueryRow(ctx, `SELECT status, ready_at FROM waitlist WHERE id = $1`, holdA).Scan(&sa, &readyAt)
	_ = pool.QueryRow(ctx, `SELECT status FROM waitlist WHERE id = $1`, holdB).Scan(&sb)
	if sa != "ready" || !readyAt.Equal(now) || sb != "waiting" {
		t.Fatalf("a=%s (%v) b=%s", sa, readyAt, sb)
	}
	var to, source string
	_ = pool.QueryRow(ctx, `SELECT to_status, source FROM copy_events WHERE copy_id = $1 ORDER BY id DESC LIMIT 1`, copyID).Scan(&to, &source)
	if to != "reserved_waitlist" || source != "self_return" {
		t.Fatalf("event →%s %s", to, source)
	}
}

// ── Borrow ────────────────────────────────────────────────────────────────────

func TestBorrowCopyAvailable(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	borrower := newUser(t, pool, "patron@athenaeum.dev")

	// Use an available copy (C02 or C03 — C01 is already checked_out in seed).
	var copyID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C02'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	d, err := repo.BorrowCopy(ctx, borrower, copyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Loan.UserID != borrower || d.Loan.CopyID != copyID || !d.Loan.BorrowedAt.Equal(now) {
		t.Fatalf("loan %+v", d.Loan)
	}
	if !d.Loan.DueAt.Equal(now.Add(model.LoanPeriod)) {
		t.Fatalf("due_at %v want %v", d.Loan.DueAt, now.Add(model.LoanPeriod))
	}
	if d.Loan.Renewals != 0 || !d.Loan.Renewable {
		t.Fatalf("renewals/renewable %d/%v", d.Loan.Renewals, d.Loan.Renewable)
	}
	if d.Copy.Status != model.CopyCheckedOut {
		t.Fatalf("copy status %s", d.Copy.Status)
	}

	// copy_event recorded.
	var from, to, source string
	_ = pool.QueryRow(ctx, `SELECT from_status, to_status, source FROM copy_events
		WHERE copy_id = $1 ORDER BY id DESC LIMIT 1`, copyID).Scan(&from, &to, &source)
	if from != "available" || to != "checked_out" || source != "self_checkout" {
		t.Fatalf("event %s→%s %s", from, to, source)
	}

	// loan row persisted.
	var lID uuid.UUID
	_ = pool.QueryRow(ctx, `SELECT id FROM loans WHERE copy_id=$1 AND returned_at IS NULL`, copyID).Scan(&lID)
	if lID != d.Loan.ID {
		t.Fatalf("loan not found in db")
	}
}

func TestBorrowCopyAlreadyCheckedOut(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	borrower := newUser(t, pool, "borrower@athenaeum.dev")

	var copyID uuid.UUID
	// C01 is already checked_out in seeded data.
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C01'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	_, err := repo.BorrowCopy(ctx, borrower, copyID, time.Now().UTC())
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeConflict {
		t.Fatalf("want CONFLICT, got %v", err)
	}
	if de.Extra["current_status"] != "checked_out" {
		t.Fatalf("current_status %v", de.Extra)
	}
}

func TestBorrowLimitEnforced(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	borrower := newUser(t, pool, "heavyborrower@athenaeum.dev")

	// Create 5 open loans manually to hit the limit.
	for i := 0; i < model.MaxOpenLoans; i++ {
		// Use a dummy copy_id — we only care about the count.
		dummyCopyID := uuid.New()
		// Insert a fake book_copy record so FK is satisfied.
		var dummyBookID uuid.UUID
		_ = pool.QueryRow(ctx, `SELECT id FROM books LIMIT 1`).Scan(&dummyBookID)
		if _, err := pool.Exec(ctx,
			`INSERT INTO book_copies (id,book_id,barcode,status) VALUES ($1,$2,$3,'checked_out')`,
			dummyCopyID, dummyBookID, "ATH-999900-C"+fmt.Sprintf("%02d", i+1)); err != nil {
			t.Fatalf("insert dummy copy: %v", err)
		}
		seedOpenLoan(t, pool, dummyCopyID, borrower, time.Now().UTC())
	}

	// Try to borrow an available copy — should hit limit.
	var availCopyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C02'`).Scan(&availCopyID); err != nil {
		t.Fatal(err)
	}
	_, err := repo.BorrowCopy(ctx, borrower, availCopyID, time.Now().UTC())
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeBorrowLimit {
		t.Fatalf("want BORROW_LIMIT, got %v", err)
	}
}

func TestBorrowPicksUpReservedWaitlistHold(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "patron-hold@athenaeum.dev")
	book := bookID(t, pool, "Neuromancer")

	// Use an AVAILABLE copy (C02) so there is no pre-existing open loan to conflict with.
	// Set it to reserved_waitlist and create a ready hold for patron.
	var reservedID uuid.UUID
	if err := pool.QueryRow(ctx, `UPDATE book_copies SET status='reserved_waitlist'
		WHERE id=(SELECT id FROM book_copies WHERE book_id=$1 AND status='available' ORDER BY barcode LIMIT 1) RETURNING id`, book).Scan(&reservedID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist (id,book_id,user_id,status,ready_at,expires_at) VALUES ($1,$2,$3,'ready',now(),now()+interval '48 hours')`,
		uuid.New(), book, patron); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	d, err := repo.BorrowCopy(ctx, patron, reservedID, now)
	if err != nil {
		t.Fatalf("borrow reserved: %v", err)
	}
	if d.Copy.Status != model.CopyCheckedOut {
		t.Fatalf("copy status %s", d.Copy.Status)
	}
	// Hold becomes picked_up.
	var holdStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM waitlist WHERE book_id=$1 AND user_id=$2`, book, patron).Scan(&holdStatus)
	if holdStatus != "picked_up" {
		t.Fatalf("hold status %s", holdStatus)
	}
}

// ── Renew ─────────────────────────────────────────────────────────────────────

func TestRenewLoan(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "renewer@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C02'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	d, err := repo.BorrowCopy(ctx, patron, copyID, now)
	if err != nil {
		t.Fatal(err)
	}

	renewNow := now.Add(time.Hour)
	rd, err := repo.RenewLoan(ctx, patron, copyID, renewNow)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if rd.Renewals != 1 || !rd.DueAt.Equal(renewNow.Add(model.LoanPeriod)) {
		t.Fatalf("renew data %+v", rd)
	}
	// Check DB.
	var renewals int
	var dueAt time.Time
	_ = pool.QueryRow(ctx, `SELECT renewals, due_at FROM loans WHERE id=$1`, d.Loan.ID).Scan(&renewals, &dueAt)
	if renewals != 1 || !dueAt.Truncate(time.Microsecond).Equal(renewNow.Add(model.LoanPeriod).Truncate(time.Microsecond)) {
		t.Fatalf("db renewals=%d due_at=%v", renewals, dueAt)
	}

	// Second renew must fail (renewals >= MaxRenewals).
	_, err = repo.RenewLoan(ctx, patron, copyID, renewNow.Add(time.Hour))
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeConflict {
		t.Fatalf("second renew: %v", err)
	}
}

func TestRenewByWrongUser(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "owner@athenaeum.dev")
	other := newUser(t, pool, "other@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C02'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BorrowCopy(ctx, patron, copyID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, err := repo.RenewLoan(ctx, other, copyID, time.Now().UTC())
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeForbidden {
		t.Fatalf("want FORBIDDEN, got %v", err)
	}
}

// ── ListLoans ─────────────────────────────────────────────────────────────────

func TestListLoansActiveAndPast(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "list-patron@athenaeum.dev")

	var c1, c2 uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C02'`).Scan(&c1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C03'`).Scan(&c2); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	// Borrow both.
	d1, err := repo.BorrowCopy(ctx, patron, c1, now)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := repo.BorrowCopy(ctx, patron, c2, now)
	if err != nil {
		t.Fatal(err)
	}

	// Return c2 — it becomes a past loan.
	_, err = repo.ReturnCopy(ctx, patron, c2, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	items, meta, err := repo.ListLoans(ctx, patron, now)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Active != 1 || meta.Overdue != 0 {
		t.Fatalf("meta %+v", meta)
	}
	if len(items) != 2 {
		t.Fatalf("items count %d", len(items))
	}
	// Active item first.
	if items[0].Kind != "active" || items[0].ID != d1.Loan.ID {
		t.Fatalf("first item %+v", items[0])
	}
	if items[1].Kind != "past" || items[1].ID != d2.Loan.ID {
		t.Fatalf("second item %+v", items[1])
	}
	if items[0].Book.Title == "" || items[0].Barcode == "" {
		t.Fatalf("book/barcode missing: %+v", items[0])
	}
	if items[0].Renewable != true || items[1].Renewable != false {
		t.Fatalf("renewable flags: %v %v", items[0].Renewable, items[1].Renewable)
	}
}

func TestListLoansOverdueFlag(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "overdue-patron@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C02'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	// Borrow well in the past so it's now overdue.
	borrowedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if _, err := repo.BorrowCopy(ctx, patron, copyID, borrowedAt); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	items, meta, err := repo.ListLoans(ctx, patron, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Overdue {
		t.Fatalf("overdue not set: %+v meta %+v", items, meta)
	}
	if meta.Overdue != 1 {
		t.Fatalf("meta.Overdue %d", meta.Overdue)
	}
}

// ── OpenLoanByCopy / CountOpenLoansByUser ─────────────────────────────────────

func TestOpenLoanByCopyAndCount(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	patron := newUser(t, pool, "scan-patron@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode='ATH-002941-C02'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}

	// No open loan yet.
	ref, err := repo.OpenLoanByCopy(ctx, copyID, patron)
	if err != nil || ref != nil {
		t.Fatalf("no open loan: %v %+v", err, ref)
	}
	count, err := repo.CountOpenLoansByUser(ctx, patron)
	if err != nil || count != 0 {
		t.Fatalf("count %d %v", count, err)
	}

	// Borrow.
	now := time.Now().UTC()
	d, err := repo.BorrowCopy(ctx, patron, copyID, now)
	if err != nil {
		t.Fatal(err)
	}

	// Now open loan is found; mine=true.
	ref, err = repo.OpenLoanByCopy(ctx, copyID, patron)
	if err != nil || ref == nil {
		t.Fatalf("open loan: %v %+v", err, ref)
	}
	if ref.ID != d.Loan.ID || !ref.Mine || !ref.DueAt.Equal(d.Loan.DueAt) {
		t.Fatalf("ref %+v", ref)
	}

	// mine=false for a different caller.
	other := newUser(t, pool, "other-scan@athenaeum.dev")
	ref2, _ := repo.OpenLoanByCopy(ctx, copyID, other)
	if ref2 == nil || ref2.Mine {
		t.Fatalf("mine should be false: %+v", ref2)
	}

	count, _ = repo.CountOpenLoansByUser(ctx, patron)
	if count != 1 {
		t.Fatalf("count after borrow %d", count)
	}
}
