package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// BookIDByISBN returns the catalog book carrying isbn13 (fetch-isbn conflict
// check, docs/03 §5.3).
func (r *CatalogRepository) BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM books WHERE isbn13 = $1`, isbn13).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
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
