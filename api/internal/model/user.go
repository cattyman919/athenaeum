package model

import (
	"time"

	"github.com/google/uuid"
)

// Roles (docs/04 §7).
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// User is an account row (docs/02 users). Secrets never serialize.
type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash *string   `json:"-"`
	GoogleSub    *string   `json:"-"`
	CreatedAt    time.Time `json:"-"`
}

// RefreshToken is a refresh_tokens row.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Hash      string
	ExpiresAt time.Time
}

// ConsumeResult describes what happened when a refresh token was presented.
type ConsumeResult int

// Outcomes of consuming a refresh token.
const (
	ConsumeUnknown  ConsumeResult = iota // no such token
	ConsumeOK                            // revoked just now by this call
	ConsumeReplayed                      // token already revoked → replay
	ConsumeExpired                       // token exists, not revoked, but expired
)
