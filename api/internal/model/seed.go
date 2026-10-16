package model

import "time"

// Fixture types for api/mocks/*.json (docs/08 §2–3).

// BuildingFixture is mocks/building.json.
type BuildingFixture struct {
	Building BuildingInfo   `json:"building"`
	Floors   []FloorFixture `json:"floors"`
}

// BuildingInfo is the building header.
type BuildingInfo struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// FloorFixture is one floor and its sections.
type FloorFixture struct {
	FloorNumber int              `json:"floor_number"`
	Name        string           `json:"name"`
	Sections    []SectionFixture `json:"sections"`
}

// SectionFixture is one section and its aisles.
type SectionFixture struct {
	Code   string         `json:"code"`
	Name   string         `json:"name"`
	Color  string         `json:"color"`
	Aisles []AisleFixture `json:"aisles"`
}

// AisleFixture is one aisle and its shelves.
type AisleFixture struct {
	Code    string         `json:"code"`
	Shelves []ShelfFixture `json:"shelves"`
}

// ShelfFixture is one shelf.
type ShelfFixture struct {
	Code       string `json:"code"`
	RowLevel   int    `json:"row_level"`
	HeightTier string `json:"height_tier"`
}

// BooksFixture is mocks/books.json.
type BooksFixture struct {
	Books []BookFixture `json:"books"`
}

// BookFixture is one catalog entry with its copies.
type BookFixture struct {
	Title          string        `json:"title"`
	Subtitle       *string       `json:"subtitle"`
	Authors        []string      `json:"authors"`
	ISBN13         string        `json:"isbn13"`
	Publisher      string        `json:"publisher"`
	PublishedYear  int           `json:"published_year"`
	Description    string        `json:"description"`
	SectionCode    string        `json:"section_code"`
	DeweyNum       string        `json:"dewey_num"`
	LOCNum         string        `json:"loc_num"`
	OpenLibraryKey *string       `json:"open_library_key"`
	CoverURL       string        `json:"-"`
	Copies         []CopyFixture `json:"copies"`
}

// CopyFixture is one physical copy.
type CopyFixture struct {
	Barcode   string `json:"barcode"`
	Status    string `json:"status"`
	Condition string `json:"condition"`
	ShelfPath string `json:"shelf_path"`
}

// LibraryFixture is mocks/library.json: the demo patron's personal library and holds (docs/13 §6).
type LibraryFixture struct {
	UserEmail string                `json:"user_email"`
	Entries   []LibraryEntryFixture `json:"entries"`
	Waitlist  []string              `json:"waitlist"` // book titles
}

// LibraryEntryFixture is one seeded user_library row, keyed by book title.
type LibraryEntryFixture struct {
	Title       string `json:"title"`
	Status      string `json:"status"`
	IsFavorite  bool   `json:"is_favorite"`
	ProgressPct *int   `json:"progress_pct"`
	Rating      *int   `json:"rating"`
}

// SeedCounts are the totals printed after seeding (docs/08 §4 step 5).
type SeedCounts struct {
	Users, Books, Copies, Shelves, CheckedOut, ZeroCopyBooks int
	LibraryEntries, WaitlistRows, Loans                      int
}

// IndexBook is a book to be chunked and embedded (docs/06 §2).
type IndexBook struct {
	ID          string
	Title       string
	Authors     []string
	Description string
}

// ChunkRow is one book_embeddings row to write.
type ChunkRow struct {
	BookID         string
	ChunkIndex     int
	Content        string
	Embedding      []float32
	Model          string
	ChunkerVersion string
}

// StaleBook reports a book whose embedding rows differ from the current model/chunker.
type StaleBook struct {
	ID     string
	Title  string
	Chunks int
}

// PendingBook is a book queued for the auto-index worker (docs/06 §2): flagged
// embedding_dirty, or with a non-empty description and no embedding rows yet.
// UpdatedAt is the snapshot used to clear the flag only when unchanged since.
type PendingBook struct {
	ID        string
	Dirty     bool
	UpdatedAt time.Time
}

// IndexStatus is the auto-index backlog snapshot (GET /api/admin/index-status,
// docs/03 §8.1): the full pending count plus the first pending ids in worker order.
type IndexStatus struct {
	PendingCount   int      `json:"pending_count"`
	PendingBookIDs []string `json:"pending_book_ids"`
}
