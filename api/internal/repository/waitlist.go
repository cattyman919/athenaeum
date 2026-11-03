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

// ReadyHold is how long a promoted waitlist entry may collect its copy (docs/12 §10).
const ReadyHold = 48 * time.Hour

// WaitlistRepository implements waitlist.Repository.
type WaitlistRepository struct{ pool *pgxpool.Pool }

// NewWaitlistRepository wraps a pool.
func NewWaitlistRepository(pool *pgxpool.Pool) *WaitlistRepository {
	return &WaitlistRepository{pool: pool}
}

const positionSQL = `SELECT count(*)::int FROM waitlist w2
	WHERE w2.book_id = $1 AND w2.status IN ('waiting','ready')
	  AND (w2.created_at < $2 OR (w2.created_at = $2 AND w2.id <= $3))`

// Join adds the user to the book's queue, returning the existing active row when present.
func (r *WaitlistRepository) Join(ctx context.Context, userID, bookID uuid.UUID) (model.WaitlistEntry, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.WaitlistEntry{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize concurrent joins for the same (user, book).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, userID.String()+bookID.String()); err != nil {
		return model.WaitlistEntry{}, false, err
	}
	var e model.WaitlistEntry
	var createdAt time.Time
	created := false
	err = tx.QueryRow(ctx, `SELECT id, status, created_at, ready_at, expires_at FROM waitlist
		WHERE user_id = $1 AND book_id = $2 AND status IN ('waiting','ready')`, userID, bookID).
		Scan(&e.ID, &e.Status, &createdAt, &e.ReadyAt, &e.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		created = true
		e.ID = uuid.New()
		e.Status = model.WaitWaiting
		err = tx.QueryRow(ctx, `INSERT INTO waitlist (id, book_id, user_id) VALUES ($1,$2,$3) RETURNING created_at`,
			e.ID, bookID, userID).Scan(&createdAt)
	}
	if err != nil {
		return model.WaitlistEntry{}, false, fmt.Errorf("join waitlist: %w", err)
	}
	if err := tx.QueryRow(ctx, positionSQL, bookID, createdAt, e.ID).Scan(&e.Position); err != nil {
		return model.WaitlistEntry{}, false, fmt.Errorf("waitlist position: %w", err)
	}
	return e, created, tx.Commit(ctx)
}

// Leave cancels the user's active entry; a cancelled `ready` hold is handed on or released.
func (r *WaitlistRepository) Leave(ctx context.Context, userID, bookID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var prev string
	err = tx.QueryRow(ctx, `
		WITH old AS (SELECT id, status FROM waitlist WHERE user_id = $1 AND book_id = $2 AND status IN ('waiting','ready') FOR UPDATE)
		UPDATE waitlist w SET status = 'cancelled' FROM old WHERE w.id = old.id RETURNING old.status`, userID, bookID).Scan(&prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx) // nothing active: idempotent
	}
	if err != nil {
		return fmt.Errorf("leave waitlist: %w", err)
	}
	if prev == model.WaitReady {
		if _, _, err := advance(ctx, tx, bookID, time.Now()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// List returns the user's active holds with queue positions.
func (r *WaitlistRepository) List(ctx context.Context, userID uuid.UUID) ([]model.WaitlistItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.id, w.status, w.ready_at, w.expires_at,
		       (SELECT count(*)::int FROM waitlist w2 WHERE w2.book_id = w.book_id AND w2.status IN ('waiting','ready')
		          AND (w2.created_at < w.created_at OR (w2.created_at = w.created_at AND w2.id <= w.id))),
		       b.id, b.title, `+coverURLSQL("b")+`, b.authors
		FROM waitlist w JOIN books b ON b.id = w.book_id
		WHERE w.user_id = $1 AND w.status IN ('waiting','ready')
		ORDER BY w.created_at, w.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list waitlist: %w", err)
	}
	defer rows.Close()
	out := []model.WaitlistItem{}
	for rows.Next() {
		it := model.WaitlistItem{Kind: "hold"}
		var authors []byte
		if err := rows.Scan(&it.ID, &it.Status, &it.ReadyAt, &it.ExpiresAt, &it.Position,
			&it.Book.ID, &it.Book.Title, &it.Book.CoverURL, &authors); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(authors, &it.Book.Authors); err != nil {
			return nil, err
		}
		if it.Book.Authors == nil {
			it.Book.Authors = []string{}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Sweep expires `ready` entries past their 48 h window and hands each freed hold to the next
// waiting patron, or releases a reserved copy back to the shelf (docs/12 §10).
func (r *WaitlistRepository) Sweep(ctx context.Context, now time.Time) (model.SweepResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.SweepResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE waitlist SET status = 'expired'
		WHERE status = 'ready' AND expires_at IS NOT NULL AND expires_at < $1 RETURNING book_id`, now)
	if err != nil {
		return model.SweepResult{}, fmt.Errorf("expire holds: %w", err)
	}
	var books []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return model.SweepResult{}, err
		}
		books = append(books, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return model.SweepResult{}, err
	}
	res := model.SweepResult{Expired: len(books)}
	for _, b := range books {
		promoted, released, err := advance(ctx, tx, b, now)
		if err != nil {
			return model.SweepResult{}, err
		}
		if promoted {
			res.Promoted++
		}
		if released {
			res.Released++
		}
	}
	return res, tx.Commit(ctx)
}

// advance gives a freed hold to the oldest waiting patron (waiting → ready, 48 h window),
// or, with nobody waiting, releases one reserved_waitlist copy back to `available`.
func advance(ctx context.Context, tx pgx.Tx, bookID uuid.UUID, now time.Time) (promoted, released bool, err error) {
	tag, err := tx.Exec(ctx, `
		UPDATE waitlist SET status = 'ready', ready_at = $2, expires_at = $3
		WHERE id = (SELECT id FROM waitlist WHERE book_id = $1 AND status = 'waiting'
		            ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)`, bookID, now, now.Add(ReadyHold))
	if err != nil {
		return false, false, fmt.Errorf("promote waitlist: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return true, false, nil
	}
	var copyID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE book_copies SET status = 'available', last_state_change = $2
		WHERE id = (SELECT id FROM book_copies WHERE book_id = $1 AND status = 'reserved_waitlist'
		            ORDER BY last_state_change, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id`, bookID, now).Scan(&copyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("release reserved copy: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO copy_events (copy_id, from_status, to_status, source)
		VALUES ($1, 'reserved_waitlist', 'available', 'waitlist')`, copyID); err != nil {
		return false, false, fmt.Errorf("copy event: %w", err)
	}
	return false, true, nil
}
