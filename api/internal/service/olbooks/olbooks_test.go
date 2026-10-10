package olbooks_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
)

const found = `{"ISBN:9780441013593":{"title":"Dune","authors":[{"name":"Frank Herbert"}],
"publishers":[{"name":"Ace"}],"publish_date":"August 1, 1990","key":"/books/OL1M",
"cover":{"large":"https://covers.example/dune-L.jpg"},
"classifications":{"dewey_decimal_class":["813/.54"],"lc_classifications":["PS3558.E63 D8"]}}}`

func TestLookupFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bibkeys") != "ISBN:9780441013593" {
			t.Errorf("query %v", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(found))
	}))
	defer srv.Close()
	md, err := olbooks.New(srv.URL, false).Lookup(context.Background(), "9780441013593")
	if err != nil {
		t.Fatal(err)
	}
	if md.Title != "Dune" || md.Publisher != "Ace" || md.Year != 1990 || md.Dewey != "813/.54" ||
		md.LOC != "PS3558.E63 D8" || md.Key != "/books/OL1M" || md.CoverURL != "https://covers.example/dune-L.jpg" ||
		len(md.Authors) != 1 {
		t.Fatalf("%+v", md)
	}
}

func TestLookupNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	if _, err := olbooks.New(srv.URL, false).Lookup(context.Background(), "9788100000021"); !errors.Is(err, olbooks.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	srv404 := httptest.NewServer(http.NotFoundHandler())
	defer srv404.Close()
	if _, err := olbooks.New(srv404.URL, false).Lookup(context.Background(), "9788100000021"); !errors.Is(err, olbooks.ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
}

func TestLookupRetriesThenFallsBack(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := olbooks.New(srv.URL, false).WithRetry(2, time.Millisecond).Lookup(context.Background(), "9780441013593")
	if !errors.Is(err, olbooks.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("attempts %d, want 1+2 retries", calls.Load())
	}
}

func TestLookupRecoversAfterTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(found))
	}))
	defer srv.Close()
	md, err := olbooks.New(srv.URL, false).WithRetry(2, time.Millisecond).Lookup(context.Background(), "9780441013593")
	if err != nil || md.Title != "Dune" {
		t.Fatalf("%+v %v", md, err)
	}
}

func TestOfflineModeMakesNoRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	if _, err := olbooks.New(srv.URL, true).Lookup(context.Background(), "9780441013593"); !errors.Is(err, olbooks.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("offline mode hit the network")
	}
}

func TestCoverURLFor(t *testing.T) {
	if got := olbooks.CoverURLFor("9780441013593"); got != "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg" {
		t.Fatal(got)
	}
}
