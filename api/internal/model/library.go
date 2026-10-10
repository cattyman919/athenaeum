package model

import (
	"time"

	"github.com/google/uuid"
)

// Reading stages (docs/02 user_library.status).
const (
	StatusWantToRead = "want_to_read"
	StatusReading    = "reading"
	StatusRead       = "read"
	// FilterFavorites is the pseudo-status accepted by GET /api/library?status=favorites.
	FilterFavorites = "favorites"
)

// Waitlist statuses (docs/02 waitlist.status).
const (
	WaitWaiting   = "waiting"
	WaitReady     = "ready"
	WaitPickedUp  = "picked_up"
	WaitExpired   = "expired"
	WaitCancelled = "cancelled"
)

// BookRef is the compact book payload used by library and waitlist items.
type BookRef struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	CoverURL *string   `json:"cover_url"`
	Authors  []string  `json:"authors"`
}

// LibraryLocation is the home shelf shown on library rows.
type LibraryLocation struct {
	Floor     int    `json:"floor"`
	ShelfCode string `json:"shelf_code"`
	Path      string `json:"path"`
}

// LibraryEntry is one user_library row.
type LibraryEntry struct {
	BookID      uuid.UUID `json:"book_id"`
	Status      string    `json:"status"`
	IsFavorite  bool      `json:"is_favorite"`
	ProgressPct *int      `json:"progress_pct"`
	Rating      *int      `json:"rating"`
	AddedAt     time.Time `json:"added_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// LibraryItem is one row of GET /api/library (docs/03 §6.1).
type LibraryItem struct {
	Kind        string           `json:"kind"`
	Book        BookRef          `json:"book"`
	Location    *LibraryLocation `json:"location"`
	Status      string           `json:"status"`
	IsFavorite  bool             `json:"is_favorite"`
	ProgressPct *int             `json:"progress_pct"`
	Rating      *int             `json:"rating"`
	AddedAt     time.Time        `json:"added_at"`
}

// LibraryCounts feed the KPI cards (additive `counts` field).
type LibraryCounts struct {
	Total      int `json:"total"`
	Reading    int `json:"reading"`
	Read       int `json:"read"`
	WantToRead int `json:"want_to_read"`
	Favorites  int `json:"favorites"`
}

// Optional marks a PATCH field as absent, null (clear) or set.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// LibraryPatch is a partial library update.
type LibraryPatch struct {
	Status      *string
	IsFavorite  *bool
	ProgressPct Optional[int]
	Rating      Optional[int]
}

// LibraryFilter selects rows for GET /api/library.
type LibraryFilter struct {
	Statuses  []string // want_to_read | reading | read
	Favorites bool
}

// WaitlistEntry is one waitlist row with its queue position.
type WaitlistEntry struct {
	ID        uuid.UUID  `json:"id"`
	Status    string     `json:"status"`
	Position  int        `json:"position"`
	ReadyAt   *time.Time `json:"ready_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// WaitlistItem is one row of GET /api/waitlist (docs/03 §6.3).
type WaitlistItem struct {
	WaitlistEntry
	Kind string  `json:"kind"`
	Book BookRef `json:"book"`
}

// HoldItem mirrors a waitlist row inside GET /api/library?include_waitlist=true.
type HoldItem struct {
	Kind     string        `json:"kind"`
	Book     BookRef       `json:"book"`
	Waitlist WaitlistEntry `json:"waitlist"`
}

// SweepResult reports what the waitlist expiry sweep changed.
type SweepResult struct {
	Expired  int
	Promoted int
	Released int
}
