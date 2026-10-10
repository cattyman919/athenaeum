package model

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
}
