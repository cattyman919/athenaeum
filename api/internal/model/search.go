package model

import "github.com/google/uuid"

// Search modes (docs/03 §5.1).
const (
	ModeKeyword  = "keyword"
	ModeSemantic = "semantic"
	ModeHybrid   = "hybrid"
)

// Availability states (docs/03 §5.1).
const (
	StateAvailable     = "available"
	StateCheckedOut    = "checked_out"
	StateNotInBuilding = "not_in_building"
	StateMissingLost   = "missing_lost"
	StateReserved      = "reserved_waitlist"
)

// SearchFilters are the facet filters shared by every search mode.
type SearchFilters struct {
	Sections      []string // section codes
	AvailableOnly bool
	Eras          []string // pre1800, 1800_1970, 1970_2000, 2000_n
}

// SearchHit is a ranked book id.
type SearchHit struct {
	BookID uuid.UUID
	Score  float64
}

// SectionRef is a section reference embedded in book payloads.
type SectionRef struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// LocationRef is the home shelf of a book.
type LocationRef struct {
	Path      string `json:"path"`
	ShelfCode string `json:"shelf_code"`
}

// Availability summarizes a book's copies.
type Availability struct {
	TotalCopies     int     `json:"total_copies"`
	AvailableCopies int     `json:"available_copies"`
	State           string  `json:"state"`
	Soon            *string `json:"soon"`
}

// BookSummary is one search result item (docs/03 §5.1).
type BookSummary struct {
	ID            uuid.UUID    `json:"id"`
	Title         string       `json:"title"`
	Authors       []string     `json:"authors"`
	ISBN13        *string      `json:"isbn13"`
	CoverURL      *string      `json:"cover_url"`
	Section       *SectionRef  `json:"section"`
	Location      *LocationRef `json:"location"`
	Availability  Availability `json:"availability"`
	PublishedYear *int         `json:"published_year"`
	Score         float64      `json:"score"`

	// Statuses carries DISTINCT copy statuses for state resolution (not serialized).
	Statuses []string `json:"-"`
}

// PageMeta is the pagination envelope meta (docs/03 §1).
type PageMeta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// SearchPage is the search response.
type SearchPage struct {
	Items []BookSummary `json:"items"`
	Meta  PageMeta      `json:"meta"`
}
