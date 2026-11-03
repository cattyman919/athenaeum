package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/service/covers"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
)

// The real olbooks client against the stub (same wire contract as openlibrary.org).
func TestStubOpenLibrary(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()
	c := olbooks.New(srv.URL, false).WithRetry(0, 0)
	md, err := c.Lookup(context.Background(), StubISBN)
	if err != nil {
		t.Fatal(err)
	}
	if md.Title != "Foundation" || len(md.Authors) != 1 || md.Authors[0] != "Isaac Asimov" ||
		md.Year != 1991 || md.Publisher != "Bantam Spectra" || md.Dewey != "813.54" || md.Key != "/books/OL7353617M" {
		t.Fatalf("%+v", md)
	}
	if _, err := c.Lookup(context.Background(), "9780441013593"); !errors.Is(err, olbooks.ErrNotFound) {
		t.Fatalf("unknown isbn: %v", err)
	}
}

// The real cover fetcher against the stub's covers.openlibrary.org stand-in.
func TestStubCovers(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()
	f := covers.NewHTTPFetcher(srv.URL, false)
	c, err := f.Fetch(context.Background(), olbooks.CoverURLFor("9780441013593"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ContentType != "image/jpeg" || len(c.Bytes) < 1024 || len(c.SHA256) != 64 ||
		c.SourceURL != olbooks.CoverURLFor("9780441013593") {
		t.Fatalf("cover: type=%s len=%d sha=%s src=%s", c.ContentType, len(c.Bytes), c.SHA256, c.SourceURL)
	}
}
