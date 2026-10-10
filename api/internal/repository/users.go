// Package repository holds pgx-backed persistence (docs/01 §2).
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

const pgUniqueViolation = "23505"

// UserRepository implements auth.Users.
type UserRepository struct{ pool *pgxpool.Pool }

// NewUserRepository wraps a pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository { return &UserRepository{pool: pool} }

const userCols = `id, email::text, name, role, password_hash, google_sub, created_at`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.GoogleSub, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, model.New(model.CodeNotFound, "User not found.")
	}
	if err != nil {
		return model.User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// Create inserts a user; duplicate email → EMAIL_TAKEN.
func (r *UserRepository) Create(ctx context.Context, u model.User) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, google_sub, name, role) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Email, u.PasswordHash, u.GoogleSub, u.Name, u.Role)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return model.New(model.CodeEmailTaken, "An account with this email already exists.")
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// ByEmail looks up by case-insensitive email.
func (r *UserRepository) ByEmail(ctx context.Context, email string) (model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
}

// ByID looks up by primary key.
func (r *UserRepository) ByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// ByGoogleSub looks up by Google subject.
func (r *UserRepository) ByGoogleSub(ctx context.Context, sub string) (model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE google_sub = $1`, sub))
}

// LinkGoogle attaches a Google subject to an existing account.
func (r *UserRepository) LinkGoogle(ctx context.Context, id uuid.UUID, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET google_sub = $2 WHERE id = $1`, id, sub)
	if err != nil {
		return fmt.Errorf("link google: %w", err)
	}
	return nil
}
