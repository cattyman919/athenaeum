package model

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

// SeedCounts are the totals printed after seeding (docs/08 §4 step 5).
type SeedCounts struct {
	Users, Books, Copies, Shelves, CheckedOut, ZeroCopyBooks int
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
