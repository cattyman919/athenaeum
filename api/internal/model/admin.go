package model

import (
	"time"

	"github.com/google/uuid"
)

// Copy statuses (docs/02 book_copies CHECK).
const (
	CopyAvailable        = "available"
	CopyCheckedOut       = "checked_out"
	CopyReservedWaitlist = "reserved_waitlist"
	CopyMissingLost      = "missing_lost"
)

// Copy event sources (docs/02 copy_events.source).
const (
	SourceAdminPatch   = "admin_patch"
	SourceSelfReturn   = "self_return"
	SourceSelfCheckout = "self_checkout"
)

// ShelfSlots is the demo capacity per shelf in the manifest inspector (docs/13 §4).
const ShelfSlots = 15

// AdminKPIs is GET /api/admin/data (docs/03 §8.1).
type AdminKPIs struct {
	TitlesInCatalog int           `json:"titles_in_catalog"`
	Copies          CopyCounts    `json:"copies"`
	ShelfCapacity   Occupancy     `json:"shelf_capacity"`
	ActivePatrons   int           `json:"active_patrons"`
	ChatWeek        ChatWeekStats `json:"chat_week"`
	ComputedAt      time.Time     `json:"computed_at"`
}

// CopyCounts aggregates book_copies by status.
type CopyCounts struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	CheckedOut  int `json:"checked_out"`
	MissingLost int `json:"missing_lost"`
}

// Occupancy is an occupied/total slot pair.
type Occupancy struct {
	Occupied int `json:"occupied"`
	Total    int `json:"total"`
}

// ChatWeekStats counts chat activity over the trailing 7 days.
type ChatWeekStats struct {
	Conversations int `json:"conversations"`
	Messages      int `json:"messages"`
}

// ActorRef identifies the user behind a copy event.
type ActorRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// CopyEvent is one audit stream row (GET /api/admin/copy-events).
type CopyEvent struct {
	ID         int64     `json:"id"`
	Barcode    string    `json:"barcode"`
	BookTitle  string    `json:"book_title"`
	FromStatus *string   `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Source     string    `json:"source"`
	Actor      *ActorRef `json:"actor"`
	CreatedAt  time.Time `json:"created_at"`
}

// AdminBook is an admin book list item / create response: the detail schema minus
// neighbors and my_library_entry, plus updated_at (docs/03 §8.2).
type AdminBook struct {
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
	OpenLibraryKey *string            `json:"open_library_key"`
	Section        *SectionRef        `json:"section"`
	Availability   DetailAvailability `json:"availability"`
	Copies         []CopyDetail       `json:"copies"`
	Wayfinding     *Wayfinding        `json:"wayfinding"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// AdminBookFromDetail projects a detail onto the admin schema.
func AdminBookFromDetail(d BookDetail, olKey *string, updatedAt time.Time) AdminBook {
	return AdminBook{
		ID: d.ID, Title: d.Title, Subtitle: d.Subtitle, Authors: d.Authors, ISBN13: d.ISBN13, ISBN10: d.ISBN10,
		Publisher: d.Publisher, PublishedYear: d.PublishedYear, Description: d.Description, DeweyNum: d.DeweyNum,
		LOCNum: d.LOCNum, CoverURL: d.CoverURL, OpenLibraryKey: olKey, Section: d.Section,
		Availability: d.Availability, Copies: d.Copies, Wayfinding: d.Wayfinding, UpdatedAt: updatedAt,
	}
}

// BookMeta is the admin-only book metadata not carried by BookDetail.
type BookMeta struct {
	OpenLibraryKey *string
	UpdatedAt      time.Time
}

// AdminBookRow is one admin list hit; the service hydrates the detail.
type AdminBookRow struct {
	ID   uuid.UUID
	Meta BookMeta
}

// AdminBookFilter filters GET /api/admin/books.
type AdminBookFilter struct {
	Search  string
	ShelfID *uuid.UUID
	SortCol string // whitelisted column
	Desc    bool
}

// BookFields are the writable book columns (POST /api/admin/books).
type BookFields struct {
	Title          string
	Subtitle       *string
	Authors        []string
	ISBN13         *string
	ISBN10         *string
	Publisher      *string
	PublishedYear  *int
	Description    *string
	CoverURL       *string
	OpenLibraryKey *string
	DeweyNum       *string
	LOCNum         *string
	SectionID      *uuid.UUID
}

// BookPatch is a partial book update (PATCH /api/admin/books/{id}).
type BookPatch struct {
	Title          *string
	Authors        *[]string
	Subtitle       Optional[string]
	ISBN13         Optional[string]
	ISBN10         Optional[string]
	Publisher      Optional[string]
	PublishedYear  Optional[int]
	Description    Optional[string]
	CoverURL       Optional[string]
	OpenLibraryKey Optional[string]
	DeweyNum       Optional[string]
	LOCNum         Optional[string]
	SectionID      Optional[uuid.UUID]
}

// TouchesRAG reports whether the patch sets a field the embeddings are built from
// (title/authors/description, docs/06 §2); other edits skip re-embedding.
func (p BookPatch) TouchesRAG() bool {
	return p.Title != nil || p.Authors != nil || p.Description.Set
}

// NewCopy is a copy to insert; an empty Barcode is allocated server-side (docs/08 §5).
type NewCopy struct {
	Barcode   string
	Status    string
	Condition string
	ShelfID   *uuid.UUID
}

// CopyPatch is a partial copy update (PATCH /api/admin/copies/{id}).
type CopyPatch struct {
	Status    *string
	Condition *string
	ShelfID   Optional[uuid.UUID]
}

// AdminCopy is a book_copies row as returned by the admin copy endpoints.
type AdminCopy struct {
	ID              uuid.UUID     `json:"id"`
	BookID          uuid.UUID     `json:"book_id"`
	Barcode         string        `json:"barcode"`
	Status          string        `json:"status"`
	Condition       string        `json:"condition"`
	ShelfID         *uuid.UUID    `json:"shelf_id"`
	Location        *CopyLocation `json:"location"`
	LastStateChange time.Time     `json:"last_state_change"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// TreeBuilding is GET /api/admin/tree (docs/03 §8.4).
type TreeBuilding struct {
	ID     uuid.UUID   `json:"id"`
	Code   string      `json:"code"`
	Name   string      `json:"name"`
	Floors []TreeFloor `json:"floors"`
}

// TreeFloor is one floor of the building tree.
type TreeFloor struct {
	ID          uuid.UUID     `json:"id"`
	FloorNumber int           `json:"floor_number"`
	Name        string        `json:"name"`
	Sections    []TreeSection `json:"sections"`
}

// TreeSection is one section of the building tree.
type TreeSection struct {
	ID     uuid.UUID   `json:"id"`
	Code   string      `json:"code"`
	Name   string      `json:"name"`
	Color  string      `json:"color"`
	Aisles []TreeAisle `json:"aisles"`
}

// TreeAisle is one aisle of the building tree.
type TreeAisle struct {
	ID      uuid.UUID   `json:"id"`
	Code    string      `json:"code"`
	Shelves []TreeShelf `json:"shelves"`
}

// TreeShelf is a leaf shelf with its occupancy aggregate.
type TreeShelf struct {
	ID         uuid.UUID `json:"id"`
	Code       string    `json:"code"`
	Path       string    `json:"path"`
	RowLevel   int       `json:"row_level"`
	HeightTier string    `json:"height_tier"`
	Occupied   int       `json:"occupied"`
	Total      int       `json:"total"`
}

// ShelfInspector is GET /api/admin/shelves/{id} (docs/03 §8.4).
type ShelfInspector struct {
	ID         uuid.UUID     `json:"id"`
	Code       string        `json:"code"`
	Path       string        `json:"path"`
	Aisle      string        `json:"aisle"`
	Floor      int           `json:"floor"`
	Section    SectionRef    `json:"section"`
	RowLevel   int           `json:"row_level"`
	HeightTier string        `json:"height_tier"`
	Occupancy  Occupancy     `json:"occupancy"`
	Copies     []ShelfCopy   `json:"copies"`
	Adjacent   []ShelfMarker `json:"adjacent"`
}

// ShelfCopy is one copy slot on a shelf.
type ShelfCopy struct {
	ID      uuid.UUID `json:"id"`
	Barcode string    `json:"barcode"`
	Status  string    `json:"status"`
	BookID  uuid.UUID `json:"book_id"`
	Title   string    `json:"title"`
}

// ShelfMarker is an adjacent shelf (same aisle) with its occupancy.
type ShelfMarker struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Path      string    `json:"path"`
	Occupancy Occupancy `json:"occupancy"`
}

// AdminUser is a GET /api/admin/users item with joined stats (docs/03 §8.5).
type AdminUser struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	Role          string     `json:"role"`
	CreatedAt     time.Time  `json:"created_at"`
	LibraryCount  int        `json:"library_count"`
	WaitlistCount int        `json:"waitlist_count"`
	LastLoginAt   *time.Time `json:"last_login_at"`
}
