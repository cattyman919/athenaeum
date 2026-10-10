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

// TokenRepository implements auth.Tokens.
type TokenRepository struct{ pool *pgxpool.Pool }

// NewTokenRepository wraps a pool.
func NewTokenRepository(pool *pgxpool.Pool) *TokenRepository { return &TokenRepository{pool: pool} }

// Insert stores a refresh token hash.
func (r *TokenRepository) Insert(ctx context.Context, t model.RefreshToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1,$2,$3,$4)`,
		t.ID, t.UserID, t.Hash, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// Consume atomically revokes a live token (UPDATE … WHERE revoked_at IS NULL)
// and classifies the outcome.
func (r *TokenRepository) Consume(ctx context.Context, hash string, now time.Time) (uuid.UUID, model.ConsumeResult, error) {
	var uid uuid.UUID
	err := r.pool.QueryRow(ctx,
		`UPDATE refresh_tokens SET revoked_at = $2
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2
		 RETURNING user_id`, hash, now).Scan(&uid)
	if err == nil {
		return uid, model.ConsumeOK, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, model.ConsumeUnknown, fmt.Errorf("consume refresh token: %w", err)
	}
	var revoked *time.Time
	err = r.pool.QueryRow(ctx, `SELECT user_id, revoked_at FROM refresh_tokens WHERE token_hash = $1`, hash).Scan(&uid, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, model.ConsumeUnknown, nil
	}
	if err != nil {
		return uuid.Nil, model.ConsumeUnknown, fmt.Errorf("lookup refresh token: %w", err)
	}
	if revoked != nil {
		return uid, model.ConsumeReplayed, nil
	}
	return uid, model.ConsumeExpired, nil
}

// RevokeAllForUser revokes every live token of the user.
func (r *TokenRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("revoke tokens: %w", err)
	}
	return nil
}
