package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// BookIDByISBN returns the catalog book carrying isbn13 via book_isbns (docs/03 §5.3,
// plan M21). book_isbns is the authoritative ISBN→book index; books.isbn13 is kept for
// display/back-compat only.
func (r *CatalogRepository) BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT book_id FROM book_isbns WHERE isbn13 = $1`, isbn13).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}

// CopyRefByBarcode resolves a copy tag to its copy and book (patron scan, docs/03 §5.4).
func (r *CatalogRepository) CopyRefByBarcode(ctx context.Context, barcode string) (copyID, bookID uuid.UUID, found bool, err error) {
	err = r.pool.QueryRow(ctx, `SELECT id, book_id FROM book_copies WHERE barcode = $1`, barcode).Scan(&copyID, &bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	return copyID, bookID, true, nil
}

// SimilarBooks returns catalog books that share a normalized title (lowercase,
// alphanumerics only) and have at least one author in common with the given
// title+authors pair (plan M21, docs/03 §5.3).
// Returns an empty slice when title is empty or no matches exist.
func (r *CatalogRepository) SimilarBooks(ctx context.Context, title string, authors []string) ([]model.SimilarBook, error) {
	if title == "" || len(authors) == 0 {
		return []model.SimilarBook{}, nil
	}
	normTitle := normalizeText(title)
	if normTitle == "" {
		return []model.SimilarBook{}, nil
	}
	// Normalize each author for comparison.
	normAuthors := make([]string, 0, len(authors))
	for _, a := range authors {
		if n := normalizeText(a); n != "" {
			normAuthors = append(normAuthors, n)
		}
	}
	if len(normAuthors) == 0 {
		return []model.SimilarBook{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT b.id, b.title, b.authors, b.isbn13,
		       (SELECT count(*) FROM book_copies bc WHERE bc.book_id = b.id)::int,
		       se.code, se.name, se.color
		FROM books b
		LEFT JOIN sections se ON se.id = b.section_id
		WHERE regexp_replace(lower(b.title), '[^a-z0-9]', '', 'g') = $1
		  AND EXISTS (
		    SELECT 1 FROM jsonb_array_elements_text(b.authors::jsonb) a(name)
		    WHERE regexp_replace(lower(a.name), '[^a-z0-9]', '', 'g') = ANY($2)
		  )
		ORDER BY b.title, b.id`, normTitle, normAuthors)
	if err != nil {
		return nil, fmt.Errorf("similar books: %w", err)
	}
	defer rows.Close()
	out := []model.SimilarBook{}
	for rows.Next() {
		var sb model.SimilarBook
		var authorsJSON []byte
		var secCode, secName, secColor *string
		if err := rows.Scan(&sb.ID, &sb.Title, &authorsJSON, &sb.ISBN13, &sb.Copies, &secCode, &secName, &secColor); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(authorsJSON, &sb.Authors); err != nil {
			sb.Authors = []string{}
		}
		if sb.Authors == nil {
			sb.Authors = []string{}
		}
		if secCode != nil {
			sb.Section = &model.SectionRef{Code: *secCode, Name: deref(secName), Color: deref(secColor)}
		}
		out = append(out, sb)
	}
	return out, rows.Err()
}

// normalizeText lowercases and strips non-alphanumeric characters; used to match
// titles and authors for deduplication (plan M21).
func normalizeText(s string) string {
	var buf []byte
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf = append(buf, byte(r))
		}
	}
	return string(buf)
}

// ISBNCache caches normalized Open Library lookups in Redis (docs/01 §2:
// "Redis: ISBN lookups").
type ISBNCache struct{ rdb redis.Cmdable }

// NewISBNCache wraps a go-redis client.
func NewISBNCache(rdb redis.Cmdable) *ISBNCache { return &ISBNCache{rdb: rdb} }

func isbnKey(isbn13 string) string { return "isbn:" + isbn13 }

// Get returns the cached record, ok=false on a miss.
func (c *ISBNCache) Get(ctx context.Context, isbn13 string) ([]byte, bool, error) {
	b, err := c.rdb.Get(ctx, isbnKey(isbn13)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// Set stores a record with a TTL.
func (c *ISBNCache) Set(ctx context.Context, isbn13 string, val []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, isbnKey(isbn13), val, ttl).Err()
}
