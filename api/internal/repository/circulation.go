package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// CirculationRepository implements circulation.Repository (patron self-service return).
type CirculationRepository struct{ pool *pgxpool.Pool }

// NewCirculationRepository wraps a pool.
func NewCirculationRepository(pool *pgxpool.Pool) *CirculationRepository {
	return &CirculationRepository{pool: pool}
}

// ReturnCopy performs a self-service return in one transaction (docs/03 §6.4): the copy row
// is locked, must be checked_out (else CONFLICT + current_status), and becomes available —
// or reserved_waitlist when the oldest waiting hold of its book is promoted to ready with a
// 48 h window. A copy_events row (source self_return, actor) records the transition.
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

	var data model.ReturnData
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
