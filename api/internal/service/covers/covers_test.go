package covers_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/covers"
)

var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{0x42}, 4096)...)

// upstream serves covers.openlibrary.org-shaped paths; path → handler behaviour.
func upstream(t *testing.T, hits *atomic.Int32, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(delay)
		if r.URL.Query().Get("default") != "false" {
			t.Errorf("open library cover fetched without default=false: %s", r.URL)
		}
		switch {
		case strings.Contains(r.URL.Path, "missing"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "pixel"):
			w.Header().Set("Content-Type", "image/gif")
			_, _ = w.Write([]byte("GIF89a tiny"))
		case strings.Contains(r.URL.Path, "html"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
		case strings.Contains(r.URL.Path, "huge"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, covers.MaxBytes+10))
		case strings.Contains(r.URL.Path, "boom"):
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpegBytes)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func olURL(name string) string { return covers.OpenLibraryCoversBase + "/b/isbn/" + name + "-L.jpg" }

func TestFetcher(t *testing.T) {
	var hits atomic.Int32
	srv := upstream(t, &hits, 0)
	f := covers.NewHTTPFetcher(srv.URL, false)
	ctx := context.Background()

	c, err := f.Fetch(ctx, olURL("9780441013593"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ContentType != "image/jpeg" || !bytes.Equal(c.Bytes, jpegBytes) || len(c.SHA256) != 64 ||
		c.SourceURL != olURL("9780441013593") {
		t.Fatalf("got %+v", c.ContentType)
	}
	for _, name := range []string{"missing", "pixel", "html", "huge"} {
		if _, err := f.Fetch(ctx, olURL(name)); !errors.Is(err, covers.ErrNoCover) {
			t.Errorf("%s: want ErrNoCover, got %v", name, err)
		}
	}
	if _, err := f.Fetch(ctx, olURL("boom")); err == nil || errors.Is(err, covers.ErrNoCover) {
		t.Errorf("boom: want transient error, got %v", err)
	}
	if _, err := f.Fetch(ctx, "ftp://example.com/x.jpg"); !errors.Is(err, covers.ErrNoCover) {
		t.Errorf("ftp: want ErrNoCover, got %v", err)
	}
	before := hits.Load()
	if _, err := covers.NewHTTPFetcher(srv.URL, true).Fetch(ctx, olURL("x")); !errors.Is(err, covers.ErrOffline) {
		t.Errorf("offline: %v", err)
	}
	if hits.Load() != before {
		t.Error("offline fetcher hit the network")
	}
}

// memStore is an in-memory covers.Store keyed like book_covers (by source URL).
type memStore struct {
	mu      sync.Mutex
	books   map[uuid.UUID]string
	covers  map[string]model.Cover
	upserts int
}

func newMemStore() *memStore {
	return &memStore{books: map[uuid.UUID]string{}, covers: map[string]model.Cover{}}
}

func (m *memStore) ForBook(_ context.Context, id uuid.UUID) (string, *model.Cover, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	src, ok := m.books[id]
	if !ok {
		return "", nil, model.New(model.CodeNotFound, "Book not found.")
	}
	if c, ok := m.covers[src]; ok {
		return src, &c, nil
	}
	return src, nil, nil
}

func (m *memStore) Upsert(_ context.Context, c model.Cover) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts++
	m.covers[c.SourceURL] = c
	return nil
}

func (m *memStore) Uncached(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, src := range m.books {
		if _, ok := m.covers[src]; !ok && src != "" && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	return out, nil
}

func (m *memStore) Prune(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	used := map[string]bool{}
	for _, src := range m.books {
		used[src] = true
	}
	var n int64
	for src := range m.covers {
		if !used[src] {
			delete(m.covers, src)
			n++
		}
	}
	return n, nil
}

func isNotFound(err error) bool {
	de, ok := model.AsError(err)
	return ok && de.Code == model.CodeNotFound
}

func TestGetReadThrough(t *testing.T) {
	var hits atomic.Int32
	srv := upstream(t, &hits, 50*time.Millisecond)
	st := newMemStore()
	dune, missing, bare := uuid.New(), uuid.New(), uuid.New()
	st.books[dune] = olURL("9780441013593")
	st.books[missing] = olURL("missing")
	st.books[bare] = ""
	svc := covers.New(st, covers.NewHTTPFetcher(srv.URL, false), logging.Discard())
	ctx := context.Background()

	// Concurrent misses for one cover share one upstream fetch (singleflight).
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := svc.Get(ctx, dune)
			if err != nil || !bytes.Equal(c.Bytes, jpegBytes) {
				t.Errorf("get: %v", err)
			}
		}()
	}
	wg.Wait()
	if hits.Load() != 1 || st.upserts != 1 {
		t.Fatalf("want 1 fetch/1 upsert, got %d/%d", hits.Load(), st.upserts)
	}
	// Cached: no further upstream traffic.
	if _, err := svc.Get(ctx, dune); err != nil || hits.Load() != 1 {
		t.Fatalf("cached get: err=%v hits=%d", err, hits.Load())
	}

	// No cover upstream → NOT_FOUND, and the miss is remembered.
	if _, err := svc.Get(ctx, missing); !isNotFound(err) {
		t.Fatalf("missing: %v", err)
	}
	h := hits.Load()
	if _, err := svc.Get(ctx, missing); !isNotFound(err) || hits.Load() != h {
		t.Fatalf("negative cache: err=%v refetched=%v", err, hits.Load() != h)
	}

	// Book without cover_url, unknown book.
	if _, err := svc.Get(ctx, bare); !isNotFound(err) {
		t.Fatalf("bare: %v", err)
	}
	if _, err := svc.Get(ctx, uuid.New()); !isNotFound(err) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestGetOfflineServesCacheOnly(t *testing.T) {
	var hits atomic.Int32
	srv := upstream(t, &hits, 0)
	st := newMemStore()
	cached, uncached := uuid.New(), uuid.New()
	st.books[cached] = olURL("a")
	st.books[uncached] = olURL("b")
	st.covers[olURL("a")] = model.Cover{SourceURL: olURL("a"), ContentType: "image/jpeg", Bytes: jpegBytes, SHA256: "abc"}
	svc := covers.New(st, covers.NewHTTPFetcher(srv.URL, true), logging.Discard())

	if c, err := svc.Get(context.Background(), cached); err != nil || c.SHA256 != "abc" {
		t.Fatalf("cached offline: %v", err)
	}
	if _, err := svc.Get(context.Background(), uncached); !isNotFound(err) {
		t.Fatalf("uncached offline: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("offline service hit the network")
	}
}

func TestWarm(t *testing.T) {
	var hits atomic.Int32
	srv := upstream(t, &hits, 0)
	st := newMemStore()
	for _, name := range []string{"1", "2", "3", "missing", "boom"} {
		st.books[uuid.New()] = olURL(name)
	}
	st.books[uuid.New()] = olURL("1") // shared URL is fetched once
	st.covers["https://gone.example/x.jpg"] = model.Cover{SourceURL: "https://gone.example/x.jpg"}
	svc := covers.New(st, covers.NewHTTPFetcher(srv.URL, false), logging.Discard())

	res, err := svc.Warm(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if res != (covers.WarmResult{Fetched: 3, Missing: 1, Failed: 1, Pruned: 1}) {
		t.Fatalf("warm: %+v", res)
	}
	// Second run only retries what is still uncached.
	res, err = svc.Warm(context.Background(), 2)
	if err != nil || res.Fetched != 0 || res.Missing+res.Failed != 2 {
		t.Fatalf("rewarm: %+v %v", res, err)
	}
}
