package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// coverURLSQL renders the public cover_url of the book aliased `alias` (docs/03 §5.6):
// NULL when the book has no cover, otherwise the API cover route, versioned with the
// cached image's digest once the bytes are in book_covers. books.cover_url itself keeps
// the upstream URL the bytes are fetched from.
func coverURLSQL(alias string) string {
	return fmt.Sprintf(`CASE WHEN %[1]s.cover_url IS NULL THEN NULL
		ELSE '/api/books/' || %[1]s.id || '/cover' || coalesce(
		  (SELECT '?v=' || left(cv.sha256, 12) FROM book_covers cv WHERE cv.source_url = %[1]s.cover_url), '') END`, alias)
}

// CoverRepository implements covers.Store.
type CoverRepository struct{ pool *pgxpool.Pool }

// NewCoverRepository wraps a pool.
func NewCoverRepository(pool *pgxpool.Pool) *CoverRepository { return &CoverRepository{pool: pool} }

// ForBook returns the book's upstream cover URL ("" when it has none) and the cached
// image for that URL (nil on a cache miss). A missing book yields NOT_FOUND.
func (r *CoverRepository) ForBook(ctx context.Context, bookID uuid.UUID) (string, *model.Cover, error) {
	var src, ctype, sum *string
	var data []byte
	err := r.pool.QueryRow(ctx, `
		SELECT b.cover_url, bc.content_type, bc.bytes, bc.sha256
		FROM books b LEFT JOIN book_covers bc ON bc.source_url = b.cover_url
		WHERE b.id = $1`, bookID).Scan(&src, &ctype, &data, &sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, model.New(model.CodeNotFound, "Book not found.")
	}
	if err != nil {
		return "", nil, fmt.Errorf("cover for book: %w", err)
	}
	if src == nil {
		return "", nil, nil
	}
	if ctype == nil {
		return *src, nil, nil
	}
	return *src, &model.Cover{SourceURL: *src, ContentType: *ctype, Bytes: data, SHA256: deref(sum)}, nil
}

// Upsert stores (or replaces) the bytes for c.SourceURL.
func (r *CoverRepository) Upsert(ctx context.Context, c model.Cover) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO book_covers (source_url, content_type, bytes, sha256)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (source_url) DO UPDATE
		SET content_type = EXCLUDED.content_type, bytes = EXCLUDED.bytes,
		    sha256 = EXCLUDED.sha256, fetched_at = now()`,
		c.SourceURL, c.ContentType, c.Bytes, c.SHA256)
	if err != nil {
		return fmt.Errorf("upsert cover: %w", err)
	}
	return nil
}

// Uncached lists distinct books.cover_url values with no cached bytes, in a stable order.
func (r *CoverRepository) Uncached(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT b.cover_url FROM books b
		WHERE b.cover_url IS NOT NULL AND b.cover_url <> ''
		  AND NOT EXISTS (SELECT 1 FROM book_covers bc WHERE bc.source_url = b.cover_url)
		ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("uncached covers: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Prune deletes cached covers no book references any more; returns the count removed.
func (r *CoverRepository) Prune(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM book_covers bc
		WHERE NOT EXISTS (SELECT 1 FROM books b WHERE b.cover_url = bc.source_url)`)
	if err != nil {
		return 0, fmt.Errorf("prune covers: %w", err)
	}
	return tag.RowsAffected(), nil
}
