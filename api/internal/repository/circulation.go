package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// CirculationRepository implements circulation.Repository (docs/03 §6.4–6.7).
type CirculationRepository struct{ pool *pgxpool.Pool }

// NewCirculationRepository wraps a pool.
func NewCirculationRepository(pool *pgxpool.Pool) *CirculationRepository {
	return &CirculationRepository{pool: pool}
}

// ReturnCopy performs a self-service return in one transaction (docs/03 §6.4): the copy row
// is locked, must be checked_out (else CONFLICT + current_status), and becomes available —
// or reserved_waitlist when the oldest waiting hold of its book is promoted to ready with a
// 48 h window. The copy's open loan (if any) is closed (returned_at=now). A copy_events row
// (source self_return, actor) records the transition.
func (r *CirculationRepository) ReturnCopy(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.ReturnData, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.ReturnData{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var from string
	var bookID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT status, book_id FROM book_copies WHERE id = $1 FOR UPDATE`, copyID).Scan(&from, &bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReturnData{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return model.ReturnData{}, fmt.Errorf("lock copy: %w", err)
	}
	if from != model.CopyCheckedOut {
		return model.ReturnData{}, model.New(model.CodeConflict, "Copy is not checked out").
			WithExtra(map[string]any{"current_status": from})
	}

	// Close the open loan for this copy, if any.
	var data model.ReturnData
	var closedLoan model.ClosedLoanRef
	err = tx.QueryRow(ctx,
		`UPDATE loans SET returned_at = $2
		 WHERE copy_id = $1 AND returned_at IS NULL
		 RETURNING id, returned_at`,
		copyID, now).Scan(&closedLoan.ID, &closedLoan.ReturnedAt)
	switch {
	case err == nil:
		data.ClosedLoan = &closedLoan
	case !errors.Is(err, pgx.ErrNoRows):
		return model.ReturnData{}, fmt.Errorf("close loan: %w", err)
	}

	var hold model.PromotedHold
	err = tx.QueryRow(ctx, `
		UPDATE waitlist SET status = 'ready', ready_at = $2, expires_at = $3
		WHERE id = (SELECT id FROM waitlist WHERE book_id = $1 AND status = 'waiting'
		            ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, status, expires_at`, bookID, now, now.Add(ReadyHold)).Scan(&hold.ID, &hold.Status, &hold.ExpiresAt)
	switch {
	case err == nil:
		data.Hold = &hold
	case !errors.Is(err, pgx.ErrNoRows):
		return model.ReturnData{}, fmt.Errorf("promote waitlist: %w", err)
	}
	to := model.CopyAvailable
	if data.Hold != nil {
		to = model.CopyReservedWaitlist
	}

	if _, err := tx.Exec(ctx, `UPDATE book_copies SET status = $2, last_state_change = $3, updated_at = now()
		WHERE id = $1`, copyID, to, now); err != nil {
		return model.ReturnData{}, fmt.Errorf("return copy: %w", err)
	}
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx, `INSERT INTO copy_events (copy_id, from_status, to_status, source, actor_id)
		VALUES ($1,$2,$3,$4,$5)`, copyID, from, to, model.SourceSelfReturn, actorID); err != nil {
		return model.ReturnData{}, fmt.Errorf("copy event: %w", err)
	}

	if data.Copy, data.Location, err = loadCopy(ctx, tx, copyID); err != nil {
		return model.ReturnData{}, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, (count(*) FILTER (WHERE status = 'available'))::int,
		       COALESCE(array_agg(DISTINCT status), '{}')
		FROM book_copies WHERE book_id = $1`, bookID).Scan(&data.Total, &data.Available, &data.Statuses); err != nil {
		return model.ReturnData{}, fmt.Errorf("availability: %w", err)
	}
	return data, tx.Commit(ctx)
}

// BorrowCopy creates a loan in one transaction (docs/03 §6.5):
//   - Locks the copy row for UPDATE
//   - Status must be available, OR reserved_waitlist held by the caller (hold goes picked_up)
//   - Caller must have fewer than MaxOpenLoans (5) open loans
//   - Sets copy.status → checked_out, inserts a loans row, appends a copy_events row
func (r *CirculationRepository) BorrowCopy(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.BorrowData, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.BorrowData{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var from string
	var bookID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT status, book_id FROM book_copies WHERE id = $1 FOR UPDATE`, copyID).Scan(&from, &bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.BorrowData{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return model.BorrowData{}, fmt.Errorf("lock copy: %w", err)
	}

	// Validate state transition.
	switch from {
	case model.CopyAvailable:
		// OK for anyone.
	case model.CopyReservedWaitlist:
		// Only OK if the caller owns the ready hold.
		var holdUserID uuid.UUID
		err = tx.QueryRow(ctx,
			`SELECT user_id FROM waitlist WHERE book_id = $1 AND status = 'ready' LIMIT 1`,
			bookID).Scan(&holdUserID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && holdUserID != actor {
			return model.BorrowData{}, model.New(model.CodeConflict, "Copy is not available to borrow").
				WithExtra(map[string]any{"current_status": from})
		}
		if err != nil {
			return model.BorrowData{}, fmt.Errorf("check hold owner: %w", err)
		}
		// Mark the hold picked_up.
		if _, err := tx.Exec(ctx,
			`UPDATE waitlist SET status = 'picked_up', updated_at = now() WHERE book_id = $1 AND user_id = $2 AND status = 'ready'`,
			bookID, actor); err != nil {
			return model.BorrowData{}, fmt.Errorf("pick up hold: %w", err)
		}
	default:
		return model.BorrowData{}, model.New(model.CodeConflict, "Copy is not available to borrow").
			WithExtra(map[string]any{"current_status": from})
	}

	// Enforce borrow limit.
	var openLoans int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM loans WHERE user_id = $1 AND returned_at IS NULL`, actor).Scan(&openLoans); err != nil {
		return model.BorrowData{}, fmt.Errorf("count loans: %w", err)
	}
	if openLoans >= model.MaxOpenLoans {
		return model.BorrowData{}, model.New(model.CodeBorrowLimit, "You already have 5 loans").
			WithExtra(map[string]any{"limit": model.MaxOpenLoans})
	}

	// Insert the loan row.
	loanID := uuid.New()
	dueAt := now.Add(model.LoanPeriod)
	var data model.BorrowData
	data.Loan = model.LoanDetail{
		ID:         loanID,
		CopyID:     copyID,
		UserID:     actor,
		BorrowedAt: now,
		DueAt:      dueAt,
		ReturnedAt: nil,
		Renewals:   0,
		Renewable:  true,
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO loans (id, copy_id, user_id, borrowed_at, due_at, renewals) VALUES ($1,$2,$3,$4,$5,0)`,
		loanID, copyID, actor, now, dueAt); err != nil {
		return model.BorrowData{}, fmt.Errorf("insert loan: %w", err)
	}

	// Update the copy.
	if _, err := tx.Exec(ctx,
		`UPDATE book_copies SET status = 'checked_out', last_state_change = $2, updated_at = now() WHERE id = $1`,
		copyID, now); err != nil {
		return model.BorrowData{}, fmt.Errorf("update copy: %w", err)
	}

	// Append copy_event.
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO copy_events (copy_id, from_status, to_status, source, actor_id) VALUES ($1,$2,$3,$4,$5)`,
		copyID, from, model.CopyCheckedOut, model.SourceSelfCheckout, actorID); err != nil {
		return model.BorrowData{}, fmt.Errorf("copy event: %w", err)
	}

	// Load fresh copy+location.
	if data.Copy, data.Location, err = loadCopy(ctx, tx, copyID); err != nil {
		return model.BorrowData{}, err
	}

	// Book availability counts.
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, (count(*) FILTER (WHERE status = 'available'))::int,
		       COALESCE(array_agg(DISTINCT status), '{}')
		FROM book_copies WHERE book_id = $1`, bookID).Scan(&data.Total, &data.Available, &data.Statuses); err != nil {
		return model.BorrowData{}, fmt.Errorf("availability: %w", err)
	}
	return data, tx.Commit(ctx)
}

// RenewLoan extends an open loan by LoanPeriod (docs/03 §6.6). Only the borrower
// may renew and only once (renewals may not exceed MaxRenewals).
func (r *CirculationRepository) RenewLoan(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.RenewData, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.RenewData{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the copy row to serialise with concurrent borrows/returns.
	var dummy string
	err = tx.QueryRow(ctx, `SELECT status FROM book_copies WHERE id = $1 FOR UPDATE`, copyID).Scan(&dummy)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.RenewData{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return model.RenewData{}, fmt.Errorf("lock copy: %w", err)
	}

	// Load the open loan.
	var loanID, loanUserID uuid.UUID
	var renewals int
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, renewals FROM loans WHERE copy_id = $1 AND returned_at IS NULL FOR UPDATE`,
		copyID).Scan(&loanID, &loanUserID, &renewals)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.RenewData{}, model.New(model.CodeConflict, "No open loan on this copy")
	}
	if err != nil {
		return model.RenewData{}, fmt.Errorf("load loan: %w", err)
	}
	if loanUserID != actor {
		return model.RenewData{}, model.New(model.CodeForbidden, "You may only renew your own loan.")
	}
	if renewals >= model.MaxRenewals {
		return model.RenewData{}, model.New(model.CodeConflict, "Loan already renewed").
			WithExtra(map[string]any{"renewals": renewals})
	}

	newDueAt := now.Add(model.LoanPeriod)
	if _, err := tx.Exec(ctx,
		`UPDATE loans SET due_at = $2, renewals = renewals + 1 WHERE id = $1`,
		loanID, newDueAt); err != nil {
		return model.RenewData{}, fmt.Errorf("renew loan: %w", err)
	}

	return model.RenewData{ID: loanID, DueAt: newDueAt, Renewals: renewals + 1}, tx.Commit(ctx)
}

// ListLoans returns all loans for the caller: active first (soonest due_at), then
// past (newest returned_at DESC). (docs/03 §6.7).
func (r *CirculationRepository) ListLoans(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.LoanItem, model.LoansMeta, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			l.id, l.copy_id, bc.barcode, l.borrowed_at, l.due_at, l.returned_at, l.renewals,
			b.id AS book_id, b.title, b.cover_url, b.authors
		FROM loans l
		JOIN book_copies bc ON bc.id = l.copy_id
		JOIN books b ON b.id = bc.book_id
		WHERE l.user_id = $1
		ORDER BY
			(l.returned_at IS NULL) DESC,  -- active (NULL returned_at) first
			CASE WHEN l.returned_at IS NULL THEN l.due_at END ASC NULLS LAST,
			l.returned_at DESC NULLS LAST`,
		userID)
	if err != nil {
		return nil, model.LoansMeta{}, fmt.Errorf("list loans: %w", err)
	}
	defer rows.Close()

	var items []model.LoanItem
	var meta model.LoansMeta
	for rows.Next() {
		var item model.LoanItem
		var authorsJSON []byte
		var returnedAt *time.Time
		if err := rows.Scan(
			&item.ID, &item.CopyID, &item.Barcode,
			&item.BorrowedAt, &item.DueAt, &returnedAt, &item.Renewals,
			&item.Book.ID, &item.Book.Title, &item.Book.CoverURL, &authorsJSON,
		); err != nil {
			return nil, model.LoansMeta{}, fmt.Errorf("scan loan row: %w", err)
		}
		if err := json.Unmarshal(authorsJSON, &item.Book.Authors); err != nil {
			item.Book.Authors = []string{}
		}
		item.ReturnedAt = returnedAt
		if returnedAt == nil {
			item.Kind = "active"
			item.Overdue = now.After(item.DueAt)
			item.Renewable = item.Renewals < model.MaxRenewals
			meta.Active++
			if item.Overdue {
				meta.Overdue++
			}
		} else {
			item.Kind = "past"
			item.Overdue = returnedAt.After(item.DueAt)
			item.Renewable = false
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, model.LoansMeta{}, fmt.Errorf("loans rows: %w", err)
	}
	if items == nil {
		items = []model.LoanItem{} // never marshal as null
	}
	return items, meta, nil
}

// OpenLoanByCopy returns the open loan for a copy, annotated with mine=true when the
// caller is the borrower. Returns nil when there is no open loan.
func (r *CirculationRepository) OpenLoanByCopy(ctx context.Context, copyID, callerID uuid.UUID) (*model.ScanLoanRef, error) {
	var ref model.ScanLoanRef
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, due_at, renewals FROM loans WHERE copy_id = $1 AND returned_at IS NULL`,
		copyID).Scan(&ref.ID, &ref.UserID, &ref.DueAt, &ref.Renewals)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open loan: %w", err)
	}
	ref.Mine = ref.UserID == callerID
	return &ref, nil
}

// CountOpenLoansByUser returns the number of open loans for a user.
func (r *CirculationRepository) CountOpenLoansByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM loans WHERE user_id = $1 AND returned_at IS NULL`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count loans: %w", err)
	}
	return n, nil
}

// BorrowCopyForBook picks the best available copy for a book and borrows it (plan M21,
// docs/03 §6.8): caller's reserved_waitlist copy first, else lowest barcode available copy.
// Uses SELECT … FOR UPDATE SKIP LOCKED to handle concurrent requests.
func (r *CirculationRepository) BorrowCopyForBook(ctx context.Context, actor, bookID uuid.UUID, now time.Time) (model.BorrowData, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.BorrowData{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Enforce borrow limit first (cheap check before locking copies).
	var openLoans int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM loans WHERE user_id = $1 AND returned_at IS NULL`, actor).Scan(&openLoans); err != nil {
		return model.BorrowData{}, fmt.Errorf("count loans: %w", err)
	}
	if openLoans >= model.MaxOpenLoans {
		return model.BorrowData{}, model.New(model.CodeBorrowLimit, "You already have 5 loans").
			WithExtra(map[string]any{"limit": model.MaxOpenLoans})
	}

	// Pick the best copy: caller's reserved_waitlist copy first, else lowest barcode available.
	// FOR UPDATE SKIP LOCKED prevents two concurrent book borrows from picking the same copy.
	var copyID uuid.UUID
	var from string
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM book_copies
		WHERE book_id = $1
		  AND (
		    (status = 'reserved_waitlist'
		     AND EXISTS (
		       SELECT 1 FROM waitlist w
		       WHERE w.book_id = $1 AND w.user_id = $2 AND w.status = 'ready'))
		    OR status = 'available'
		  )
		ORDER BY
		  (status = 'reserved_waitlist'
		   AND EXISTS (
		     SELECT 1 FROM waitlist w
		     WHERE w.book_id = $1 AND w.user_id = $2 AND w.status = 'ready')) DESC,
		  barcode ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, bookID, actor).Scan(&copyID, &from)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.BorrowData{}, model.New(model.CodeConflict, "No copy available to borrow for this book.")
	}
	if err != nil {
		return model.BorrowData{}, fmt.Errorf("pick copy: %w", err)
	}

	// If picking the hold copy, mark it picked_up.
	if from == model.CopyReservedWaitlist {
		if _, err := tx.Exec(ctx,
			`UPDATE waitlist SET status = 'picked_up', updated_at = now()
			 WHERE book_id = $1 AND user_id = $2 AND status = 'ready'`,
			bookID, actor); err != nil {
			return model.BorrowData{}, fmt.Errorf("pick up hold: %w", err)
		}
	}

	// Insert loan row.
	loanID := uuid.New()
	dueAt := now.Add(model.LoanPeriod)
	data := model.BorrowData{
		Loan: model.LoanDetail{
			ID: loanID, CopyID: copyID, UserID: actor,
			BorrowedAt: now, DueAt: dueAt, Renewals: 0, Renewable: true,
		},
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO loans (id, copy_id, user_id, borrowed_at, due_at, renewals) VALUES ($1,$2,$3,$4,$5,0)`,
		loanID, copyID, actor, now, dueAt); err != nil {
		return model.BorrowData{}, fmt.Errorf("insert loan: %w", err)
	}

	// Update the copy.
	if _, err := tx.Exec(ctx,
		`UPDATE book_copies SET status = 'checked_out', last_state_change = $2, updated_at = now() WHERE id = $1`,
		copyID, now); err != nil {
		return model.BorrowData{}, fmt.Errorf("update copy: %w", err)
	}

	// Append copy_event.
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO copy_events (copy_id, from_status, to_status, source, actor_id) VALUES ($1,$2,$3,$4,$5)`,
		copyID, from, model.CopyCheckedOut, model.SourceSelfCheckout, actorID); err != nil {
		return model.BorrowData{}, fmt.Errorf("copy event: %w", err)
	}

	// Load fresh copy+location.
	if data.Copy, data.Location, err = loadCopy(ctx, tx, copyID); err != nil {
		return model.BorrowData{}, err
	}
	// Book availability counts.
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, (count(*) FILTER (WHERE status = 'available'))::int,
		       COALESCE(array_agg(DISTINCT status), '{}')
		FROM book_copies WHERE book_id = $1`, bookID).Scan(&data.Total, &data.Available, &data.Statuses); err != nil {
		return model.BorrowData{}, fmt.Errorf("availability: %w", err)
	}
	return data, tx.Commit(ctx)
}

// ReturnCopyForBook closes the caller's oldest open loan for any copy of the given book
// (plan M21, docs/03 §6.9). Returns CONFLICT when no open loan exists.
func (r *CirculationRepository) ReturnCopyForBook(ctx context.Context, actor, bookID uuid.UUID, now time.Time) (model.ReturnData, error) {
	// Find caller's oldest open loan for a copy of this book.
	var copyID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT l.copy_id FROM loans l
		JOIN book_copies bc ON bc.id = l.copy_id
		WHERE bc.book_id = $1 AND l.user_id = $2 AND l.returned_at IS NULL
		ORDER BY l.borrowed_at ASC, l.id ASC
		LIMIT 1`, bookID, actor).Scan(&copyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReturnData{}, model.New(model.CodeConflict, "You have no open loan for this book.")
	}
	if err != nil {
		return model.ReturnData{}, fmt.Errorf("find loan for book: %w", err)
	}
	// Reuse the existing ReturnCopy logic on the selected copy.
	return r.ReturnCopy(ctx, actor, copyID, now)
}

// BookBorrowInfo returns the borrow_copy and my_loans fields for a book-match scan result
// (plan M21). Returns nil borrow_copy when nothing is borrowable.
func (r *CirculationRepository) BookBorrowInfo(ctx context.Context, bookID, callerID uuid.UUID) (
	borrowable bool, borrowCopy *model.ScanBorrowCopy, returnable bool, myLoans []model.ScanMyLoan, err error,
) {
	myLoans = []model.ScanMyLoan{}

	// My open loans for any copy of this book.
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.copy_id, bc.barcode, l.due_at FROM loans l
		JOIN book_copies bc ON bc.id = l.copy_id
		WHERE bc.book_id = $1 AND l.user_id = $2 AND l.returned_at IS NULL
		ORDER BY l.borrowed_at ASC`, bookID, callerID)
	if err != nil {
		return false, nil, false, nil, fmt.Errorf("my book loans: %w", err)
	}
	for rows.Next() {
		var loan model.ScanMyLoan
		if err := rows.Scan(&loan.ID, &loan.CopyID, &loan.Barcode, &loan.DueAt); err != nil {
			rows.Close()
			return false, nil, false, nil, err
		}
		myLoans = append(myLoans, loan)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, false, nil, err
	}
	returnable = len(myLoans) > 0

	// Pick borrow_copy: caller's reserved_waitlist copy first, else lowest barcode available.
	var bcID uuid.UUID
	var bcBarcode string
	err = r.pool.QueryRow(ctx, `
		SELECT id, barcode FROM book_copies
		WHERE book_id = $1
		  AND (
		    (status = 'reserved_waitlist'
		     AND EXISTS (
		       SELECT 1 FROM waitlist w WHERE w.book_id = $1 AND w.user_id = $2 AND w.status = 'ready'))
		    OR status = 'available'
		  )
		ORDER BY
		  (status = 'reserved_waitlist'
		   AND EXISTS (
		     SELECT 1 FROM waitlist w WHERE w.book_id = $1 AND w.user_id = $2 AND w.status = 'ready')) DESC,
		  barcode ASC
		LIMIT 1`, bookID, callerID).Scan(&bcID, &bcBarcode)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No borrowable copy.
	case err != nil:
		return false, nil, returnable, myLoans, fmt.Errorf("borrow copy: %w", err)
	default:
		// Load location for this copy.
		var bcPath *string
		var bCode, bName, fName, sName, aisle, shelf, tier, sCode *string
		var floorN, rowLevel *int
		_ = r.pool.QueryRow(ctx, `
			SELECT bl.path, bl.building_code, bl.building_name, bl.floor_number, bl.floor_name,
			       bl.section_code, bl.section_name, bl.aisle_code, bl.shelf_code, bl.row_level, bl.height_tier
			FROM book_locations bl WHERE bl.copy_id = $1`, bcID).
			Scan(&bcPath, &bCode, &bName, &floorN, &fName, &sCode, &sName, &aisle, &shelf, &rowLevel, &tier)
		bc := &model.ScanBorrowCopy{ID: bcID, Barcode: bcBarcode}
		if bcPath != nil {
			loc := model.LocationRow{
				CopyID: bcID, BuildingCode: deref(bCode), BuildingName: deref(bName),
				FloorNumber: derefInt(floorN), FloorName: deref(fName),
				SectionCode: deref(sCode), SectionName: deref(sName),
				AisleCode: deref(aisle), ShelfCode: deref(shelf),
				RowLevel: derefInt(rowLevel), HeightTier: deref(tier), Path: *bcPath,
			}
			bc.Location = &model.Wayfinding{Path: loc.Path, Parts: model.LocationParts{
				BuildingCode: loc.BuildingCode, BuildingName: loc.BuildingName,
				FloorNumber: loc.FloorNumber, FloorName: loc.FloorName,
				SectionCode: loc.SectionCode, SectionName: loc.SectionName,
				AisleCode: loc.AisleCode, ShelfCode: loc.ShelfCode,
				RowLevel: loc.RowLevel, HeightTier: loc.HeightTier,
			}}
		}
		borrowCopy = bc
		borrowable = true
	}
	return borrowable, borrowCopy, returnable, myLoans, nil
}
