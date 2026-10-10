package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/repository"
)

func TestBookIDByISBN(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	repo := repository.NewCatalogRepository(pool)
	var want uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM books WHERE title = 'Dune'`).Scan(&want); err != nil {
		t.Fatal(err)
	}
	id, ok, err := repo.BookIDByISBN(ctx, "9780441013593")
	if err != nil || !ok || id != want {
		t.Fatalf("%v %v %v", id, ok, err)
	}
	if _, ok, err := repo.BookIDByISBN(ctx, "9780553293357"); err != nil || ok {
		t.Fatalf("unknown isbn: %v %v", ok, err)
	}
}

func TestISBNCache(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	c := repository.NewISBNCache(rdb)
	ctx := context.Background()
	if _, ok, err := c.Get(ctx, "9780553293357"); ok || err != nil {
		t.Fatalf("miss: %v %v", ok, err)
	}
	if err := c.Set(ctx, "9780553293357", []byte(`{"x":1}`), time.Hour); err != nil {
		t.Fatal(err)
	}
	b, ok, err := c.Get(ctx, "9780553293357")
	if !ok || err != nil || string(b) != `{"x":1}` || mr.TTL("isbn:9780553293357") != time.Hour {
		t.Fatalf("%s %v %v ttl=%v", b, ok, err, mr.TTL("isbn:9780553293357"))
	}
}
