// Package memauth provides in-memory auth repositories for service and
// handler tests (hand-written fakes, docs/09 §8).
package memauth

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Users is an in-memory auth.Users.
type Users struct {
	mu   sync.Mutex
	rows map[uuid.UUID]model.User
}

// NewUsers returns an empty store.
func NewUsers() *Users { return &Users{rows: map[uuid.UUID]model.User{}} }

// Create inserts, enforcing case-insensitive unique email.
func (m *Users) Create(_ context.Context, u model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if strings.EqualFold(r.Email, u.Email) {
			return model.New(model.CodeEmailTaken, "An account with this email already exists.")
		}
	}
	m.rows[u.ID] = u
	return nil
}

func (m *Users) find(pred func(model.User) bool) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if pred(r) {
			return r, nil
		}
	}
	return model.User{}, model.New(model.CodeNotFound, "User not found.")
}

// ByEmail looks up by email, case-insensitively.
func (m *Users) ByEmail(_ context.Context, e string) (model.User, error) {
	return m.find(func(u model.User) bool { return strings.EqualFold(u.Email, e) })
}

// ByID looks up by id.
func (m *Users) ByID(_ context.Context, id uuid.UUID) (model.User, error) {
	return m.find(func(u model.User) bool { return u.ID == id })
}

// ByGoogleSub looks up by Google subject.
func (m *Users) ByGoogleSub(_ context.Context, sub string) (model.User, error) {
	return m.find(func(u model.User) bool { return u.GoogleSub != nil && *u.GoogleSub == sub })
}

// LinkGoogle sets the Google subject.
func (m *Users) LinkGoogle(_ context.Context, id uuid.UUID, sub string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.rows[id]
	u.GoogleSub = &sub
	m.rows[id] = u
	return nil
}

// SetRole changes a role (test helper).
func (m *Users) SetRole(id uuid.UUID, role string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.rows[id]
	u.Role = role
	m.rows[id] = u
}

type tokenRow struct {
	model.RefreshToken
	revoked bool
}

// Tokens is an in-memory auth.Tokens.
type Tokens struct {
	mu   sync.Mutex
	rows map[string]*tokenRow
}

// NewTokens returns an empty store.
func NewTokens() *Tokens { return &Tokens{rows: map[string]*tokenRow{}} }

// Insert stores a token.
func (m *Tokens) Insert(_ context.Context, t model.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[t.Hash] = &tokenRow{RefreshToken: t}
	return nil
}

// Consume mirrors the SQL semantics of the pgx repository.
func (m *Tokens) Consume(_ context.Context, hash string, now time.Time) (uuid.UUID, model.ConsumeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[hash]
	switch {
	case !ok:
		return uuid.Nil, model.ConsumeUnknown, nil
	case r.revoked:
		return r.UserID, model.ConsumeReplayed, nil
	case !r.ExpiresAt.After(now):
		return r.UserID, model.ConsumeExpired, nil
	}
	r.revoked = true
	return r.UserID, model.ConsumeOK, nil
}

// RevokeAllForUser revokes every token of the user.
func (m *Tokens) RevokeAllForUser(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.UserID == id {
			r.revoked = true
		}
	}
	return nil
}

// Live counts unrevoked tokens of the user.
func (m *Tokens) Live(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.rows {
		if r.UserID == id && !r.revoked {
			n++
		}
	}
	return n
}
