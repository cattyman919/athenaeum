package model

// ISBNIdentification holds the patron-safe Open Library fields returned by
// POST /api/books/identify-isbn (docs/03 §5.5). Admin-only fields (dewey,
// loc, description, open_library_key) are intentionally omitted.
type ISBNIdentification struct {
	Title         string   `json:"title"`
	Authors       []string `json:"authors"`
	Publisher     *string  `json:"publisher"`
	PublishedYear *int     `json:"published_year"`
	CoverURL      *string  `json:"cover_url"`
}

// IdentifyResult is the POST /api/books/identify-isbn 200 response (docs/03 §5.5).
// Upstream failures always yield 200 with identified:null, never 5xx.
type IdentifyResult struct {
	ISBN13     string              `json:"isbn13"`
	InCatalog  bool                `json:"in_catalog"`
	BookID     *string             `json:"book_id"`
	Identified *ISBNIdentification `json:"identified"`
	Source     string              `json:"source"`
	Warnings   []string            `json:"warnings"`
}

// SimilarBook is one entry in the fetch-isbn `similar` list (plan M21, docs/03 §5.3).
// Returned when another catalog book has the same normalized title and shares ≥1 author.
type SimilarBook struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Authors []string    `json:"authors"`
	ISBN13  *string     `json:"isbn13"`
	Copies  int         `json:"copies"`
	Section *SectionRef `json:"section"`
}

// ISBNMetadata is the POST /api/books/fetch-isbn response (docs/03 §5.3).
// Existing catalog books answer 409 ISBN_CONFLICT instead, so InCatalog is
// false on every 200. Partial records (source "offline_partial") carry an
// empty title, empty covers and a warnings entry (docs/08 §6).
type ISBNMetadata struct {
	ISBN13         string   `json:"isbn13"`
	Title          string   `json:"title"`
	Authors        []string `json:"authors"`
	Publisher      *string  `json:"publisher"`
	PublishedYear  *int     `json:"published_year"`
	Description    *string  `json:"description"`
	CoverURL       *string  `json:"cover_url"`
	DeweyNum       *string  `json:"dewey_num"`
	LOCNum         *string  `json:"loc_num"`
	OpenLibraryKey *string  `json:"open_library_key"`
	Source         string   `json:"source"`
	InCatalog      bool     `json:"in_catalog"`
	Covers         []string `json:"covers"`
	Warnings       []string `json:"warnings"`
	// Similar contains catalog books whose normalized title matches and share ≥1 author
	// (plan M21). Empty array when none found or title/authors are partial.
	Similar []SimilarBook `json:"similar"`
}
