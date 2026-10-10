package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/checkdigit"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
)

// Intake sources (docs/03 §5.3, docs/08 §6).
const (
	SourceOpenLibrary    = "openlibrary"
	SourceOfflinePartial = "offline_partial"

	// WarnUpstreamUnavailable marks a partial record after Open Library failed.
	WarnUpstreamUnavailable = "upstream_unavailable"
	// WarnNotFound marks a partial record when Open Library has no entry.
	WarnNotFound = "not_found"

	// ISBNCacheTTL bounds how long normalized Open Library records stay in Redis.
	ISBNCacheTTL = 24 * time.Hour
)

// ValidateISBN normalizes (trims, drops hyphens/spaces) and verifies an
// ISBN-13 / EAN-13 check digit (docs/11 §9). Invalid → 400 INVALID_ISBN.
func ValidateISBN(raw string) (string, error) {
	s := strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if !checkdigit.ValidISBN13(s) {
		return "", model.New(model.CodeInvalidISBN, "That ISBN-13 failed checksum validation.")
	}
	return s, nil
}

// MetadataLookup is the Open Library client seam (olbooks.Client).
type MetadataLookup interface {
	Lookup(ctx context.Context, isbn13 string) (olbooks.Metadata, error)
}

// ISBNCache stores normalized lookups (Redis in production).
type ISBNCache interface {
	Get(ctx context.Context, isbn13 string) ([]byte, bool, error)
	Set(ctx context.Context, isbn13 string, val []byte, ttl time.Duration) error
}

// ISBNRepository finds existing catalog rows by ISBN.
type ISBNRepository interface {
	BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error)
}

// ISBNService implements POST /api/books/fetch-isbn (docs/03 §5.3).
type ISBNService struct {
	repo   ISBNRepository
	lookup MetadataLookup
	cache  ISBNCache
	log    *slog.Logger
}

// NewISBNService builds the intake service; cache may be nil.
func NewISBNService(repo ISBNRepository, lookup MetadataLookup, cache ISBNCache, log *slog.Logger) *ISBNService {
	if log == nil {
		log = slog.Default()
	}
	return &ISBNService{repo: repo, lookup: lookup, cache: cache, log: log}
}

// FetchISBN validates, rejects ISBNs already in the catalog (409 ISBN_CONFLICT
// + book_id) and returns normalized metadata. Upstream failures never block
// intake: they yield a 200 partial record (docs/08 §6).
func (s *ISBNService) FetchISBN(ctx context.Context, raw string) (model.ISBNMetadata, error) {
	isbn, err := ValidateISBN(raw)
	if err != nil {
		return model.ISBNMetadata{}, err
	}
	id, found, err := s.repo.BookIDByISBN(ctx, isbn)
	if err != nil {
		return model.ISBNMetadata{}, err
	}
	if found {
		return model.ISBNMetadata{}, model.New(model.CodeISBNConflict, "A book with this ISBN is already in the catalog.").
			WithExtra(map[string]any{"book_id": id.String()})
	}

	if md, ok := s.cached(ctx, isbn); ok {
		return md, nil
	}

	meta, err := s.lookup.Lookup(ctx, isbn)
	switch {
	case err == nil:
		md := fromOpenLibrary(isbn, meta)
		s.store(ctx, isbn, md)
		return md, nil
	case errors.Is(err, olbooks.ErrNotFound):
		return partial(isbn, WarnNotFound), nil
	default:
		s.log.WarnContext(ctx, "fetch-isbn upstream unavailable", "isbn", isbn, "err", err)
		return partial(isbn, WarnUpstreamUnavailable), nil
	}
}

func (s *ISBNService) cached(ctx context.Context, isbn string) (model.ISBNMetadata, bool) {
	if s.cache == nil {
		return model.ISBNMetadata{}, false
	}
	b, ok, err := s.cache.Get(ctx, isbn)
	if err != nil {
		s.log.WarnContext(ctx, "isbn cache read failed", "err", err)
		return model.ISBNMetadata{}, false
	}
	if !ok {
		return model.ISBNMetadata{}, false
	}
	var md model.ISBNMetadata
	if err := json.Unmarshal(b, &md); err != nil || md.ISBN13 != isbn {
		return model.ISBNMetadata{}, false
	}
	return md, true
}

func (s *ISBNService) store(ctx context.Context, isbn string, md model.ISBNMetadata) {
	if s.cache == nil {
		return
	}
	b, err := json.Marshal(md)
	if err != nil {
		return
	}
	if err := s.cache.Set(ctx, isbn, b, ISBNCacheTTL); err != nil {
		s.log.WarnContext(ctx, "isbn cache write failed", "err", err)
	}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fromOpenLibrary(isbn string, m olbooks.Metadata) model.ISBNMetadata {
	md := model.ISBNMetadata{
		ISBN13:         isbn,
		Title:          m.Title,
		Authors:        m.Authors,
		Publisher:      optString(m.Publisher),
		Description:    optString(m.Description),
		CoverURL:       optString(m.CoverURL),
		DeweyNum:       optString(m.Dewey),
		LOCNum:         optString(m.LOC),
		OpenLibraryKey: optString(m.Key),
		Source:         SourceOpenLibrary,
		Covers:         []string{},
		Warnings:       []string{},
	}
	if m.CoverURL != "" {
		md.Covers = []string{m.CoverURL}
	}
	if md.Authors == nil {
		md.Authors = []string{}
	}
	if m.Year > 0 {
		y := m.Year
		md.PublishedYear = &y
	}
	return md
}

func partial(isbn, warning string) model.ISBNMetadata {
	return model.ISBNMetadata{
		ISBN13:   isbn,
		Title:    "",
		Authors:  []string{},
		Source:   SourceOfflinePartial,
		Covers:   []string{},
		Warnings: []string{warning},
	}
}
