package model

import (
	"time"

	"github.com/google/uuid"
)

// CopyLocation is a copy's resolved shelf location.
type CopyLocation struct {
	Path   string `json:"path"`
	Ladder string `json:"ladder"`
}

// CopyDetail is one physical copy in the manifest (docs/03 §5.2).
type CopyDetail struct {
	ID              uuid.UUID     `json:"id"`
	Barcode         string        `json:"barcode"`
	Status          string        `json:"status"`
	Condition       string        `json:"condition"`
	LastStateChange time.Time     `json:"last_state_change"`
	Location        *CopyLocation `json:"location"`
}

// Wayfinding is the turn-by-turn route to the book's home shelf.
type Wayfinding struct {
	Path        string   `json:"path"`
	LadderSteps []string `json:"ladder_steps"`
}

// Neighbor is an "archival affinity" carousel entry.
type Neighbor struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Authors  []string  `json:"authors"`
	CoverURL *string   `json:"cover_url"`
}

// LibraryEntryRef is the caller's library state for the book.
type LibraryEntryRef struct {
	Status     string `json:"status"`
	IsFavorite bool   `json:"is_favorite"`
}

// DetailAvailability is Availability without the search-only "soon" field.
type DetailAvailability struct {
	TotalCopies     int    `json:"total_copies"`
	AvailableCopies int    `json:"available_copies"`
	State           string `json:"state"`
}

// BookDetail is GET /api/books/{id} (docs/03 §5.2).
type BookDetail struct {
	ID             uuid.UUID          `json:"id"`
	Title          string             `json:"title"`
	Subtitle       *string            `json:"subtitle"`
	Authors        []string           `json:"authors"`
	ISBN13         *string            `json:"isbn13"`
	ISBN10         *string            `json:"isbn10"`
	Publisher      *string            `json:"publisher"`
	PublishedYear  *int               `json:"published_year"`
	Description    *string            `json:"description"`
	DeweyNum       *string            `json:"dewey_num"`
	LOCNum         *string            `json:"loc_num"`
	CoverURL       *string            `json:"cover_url"`
	Section        *SectionRef        `json:"section"`
	Availability   DetailAvailability `json:"availability"`
	Copies         []CopyDetail       `json:"copies"`
	Wayfinding     *Wayfinding        `json:"wayfinding"`
	Neighbors      []Neighbor         `json:"neighbors"`
	MyLibraryEntry *LibraryEntryRef   `json:"my_library_entry"`
}

// LocationRow is a book_locations row for one copy (repository → service).
type LocationRow struct {
	CopyID       uuid.UUID
	Barcode      string
	BuildingName string
	FloorNumber  int
	FloorName    string
	SectionName  string
	AisleCode    string
	ShelfCode    string
	RowLevel     int
	HeightTier   string
	Path         string
}

// BookDetailData is the raw repository result; the service derives ladders, wayfinding
// and availability state from it.
type BookDetailData struct {
	Detail    BookDetail
	Locations map[uuid.UUID]LocationRow // keyed by copy id; absent when the copy has no shelf
	Statuses  []string                  // DISTINCT copy statuses (availability.state resolution)
}
