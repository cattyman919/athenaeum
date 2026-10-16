package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
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

func TestCopyRefByBarcodeAndScan(t *testing.T) {
	pool, _ := seededCatalog(t)
	ctx := context.Background()
	repo := repository.NewCatalogRepository(pool)
	dune := bookID(t, pool, "Dune")
	var wantCopy uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C01'`).Scan(&wantCopy); err != nil {
		t.Fatal(err)
	}
	copyID, book, ok, err := repo.CopyRefByBarcode(ctx, "ATH-002941-C01")
	if err != nil || !ok || copyID != wantCopy || book != dune {
		t.Fatalf("%v %v %v %v", copyID, book, ok, err)
	}
	if _, _, ok, err := repo.CopyRefByBarcode(ctx, "ATH-999999-C01"); err != nil || ok {
		t.Fatalf("unknown tag: %v %v", ok, err)
	}

	svc := catalog.NewScanService(repo)
	res, err := svc.Scan(ctx, "9780441013593")
	if err != nil || res.Match != model.ScanMatchBook || res.Book.ID != dune ||
		res.Availability.TotalCopies != 3 || res.Availability.AvailableCopies != 2 || res.Wayfinding.Path != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("isbn scan %+v %v", res, err)
	}
	res, err = svc.Scan(ctx, "ath-002941-c01")
	if err != nil || res.Match != model.ScanMatchCopy || res.Copy.ID != wantCopy || !res.Returnable || res.Wayfinding == nil {
		t.Fatalf("copy scan %+v %v", res, err)
	}
	res, err = svc.Scan(ctx, "9780306406157")
	if err != nil || res.InCatalog || res.Match != model.ScanMatchNone {
		t.Fatalf("unknown isbn scan %+v %v", res, err)
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
