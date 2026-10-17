package catalog_test

import (
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
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
)

const foundationISBN = "9780553293357"

const foundationJSON = `{"ISBN:9780553293357":{"title":"Foundation","key":"/books/OL7353617M",
"authors":[{"name":"Isaac Asimov"}],"publishers":[{"name":"Bantam Spectra"}],"publish_date":"1991",
"notes":"The first novel of the Foundation trilogy.",
"classifications":{"dewey_decimal_class":["813.54"],"lc_classifications":["PS3551.S5 F6 1991"]}}}`

// fakeOpenLibrary mimics GET /api/books?bibkeys=ISBN:…&jscmd=data&format=json.
func fakeOpenLibrary(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/books" || r.URL.Query().Get("jscmd") != "data" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if r.URL.Query().Get("bibkeys") == "ISBN:"+foundationISBN {
			_, _ = w.Write([]byte(foundationJSON))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

type fakeISBNRepo struct {
	byISBN  map[string]uuid.UUID
	similar []model.SimilarBook
}

func (f fakeISBNRepo) BookIDByISBN(_ context.Context, isbn string) (uuid.UUID, bool, error) {
	id, ok := f.byISBN[isbn]
	return id, ok, nil
}

func (f fakeISBNRepo) SimilarBooks(_ context.Context, _ string, _ []string) ([]model.SimilarBook, error) {
	if f.similar != nil {
		return f.similar, nil
	}
	return []model.SimilarBook{}, nil
}

type memCache struct {
	mu  sync.Mutex
	m   map[string][]byte
	ttl time.Duration
}

func (c *memCache) Get(_ context.Context, k string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok, nil
}

func (c *memCache) Set(_ context.Context, k string, v []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string][]byte{}
	}
	c.m[k], c.ttl = v, ttl
	return nil
}

func newISBN(t *testing.T, base string, repo fakeISBNRepo, cache catalog.ISBNCache) *catalog.ISBNService {
	t.Helper()
	ol := olbooks.New(base, false).WithRetry(2, time.Millisecond)
	return catalog.NewISBNService(repo, ol, cache, logging.Discard())
}

func TestValidateISBN(t *testing.T) {
	for in, want := range map[string]string{
		"9780441013593":      "9780441013593",
		" 978-0-441-01359-3": "9780441013593",
		"9780441013594":      "", // bad check digit
		"4006381333931":      "", // EAN-13 but not Bookland 978/979
		"036000291452":       "", // UPC-A
		"":                   "",
	} {
		got, err := catalog.ValidateISBN(in)
		if want == "" {
			var de *model.Error
			if !errors.As(err, &de) || de.Code != model.CodeInvalidISBN {
				t.Errorf("%q: want INVALID_ISBN, got %q %v", in, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q %v", in, got, err)
		}
	}
}

func TestFetchISBNFoundNormalizesAndCaches(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	cache := &memCache{}
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, cache)

	md, err := svc.FetchISBN(context.Background(), "978-0-553-29335-7")
	if err != nil {
		t.Fatal(err)
	}
	if md.ISBN13 != foundationISBN || md.Title != "Foundation" || md.Source != catalog.SourceOpenLibrary ||
		md.InCatalog || strings.Join(md.Authors, ",") != "Isaac Asimov" {
		t.Fatalf("%+v", md)
	}
	if *md.Publisher != "Bantam Spectra" || *md.PublishedYear != 1991 || *md.DeweyNum != "813.54" ||
		*md.LOCNum != "PS3551.S5 F6 1991" || *md.OpenLibraryKey != "/books/OL7353617M" ||
		*md.Description != "The first novel of the Foundation trilogy." ||
		*md.CoverURL != olbooks.CoverURLFor(foundationISBN) || len(md.Warnings) != 0 {
		t.Fatalf("%+v", md)
	}
	if cache.ttl != catalog.ISBNCacheTTL || len(cache.m) != 1 {
		t.Fatalf("cache not written: %v %v", cache.ttl, cache.m)
	}

	// Second lookup is served from the cache without touching Open Library.
	before := hits.Load()
	md2, err := svc.FetchISBN(context.Background(), foundationISBN)
	if err != nil || md2.Title != "Foundation" || hits.Load() != before {
		t.Fatalf("cache miss: %+v %v hits %d→%d", md2, err, before, hits.Load())
	}
}

func TestFetchISBNInvalidChecksum(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, nil)
	_, err := svc.FetchISBN(context.Background(), "9780553293358")
	var de *model.Error
	if !errors.As(err, &de) || de.Code != model.CodeInvalidISBN || hits.Load() != 0 {
		t.Fatalf("%v hits=%d", err, hits.Load())
	}
}

func TestFetchISBNConflictCarriesBookID(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	id := uuid.New()
	svc := newISBN(t, ol.URL, fakeISBNRepo{byISBN: map[string]uuid.UUID{"9780441013593": id}}, nil)
	_, err := svc.FetchISBN(context.Background(), "9780441013593")
	var de *model.Error
	if !errors.As(err, &de) || de.Code != model.CodeISBNConflict || de.Extra["book_id"] != id.String() {
		t.Fatalf("%v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("conflict must short-circuit before Open Library")
	}
}

func TestFetchISBNUpstreamFailureReturnsPartial(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusInternalServerError)
	cache := &memCache{}
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, cache)
	md, err := svc.FetchISBN(context.Background(), foundationISBN)
	if err != nil {
		t.Fatal(err)
	}
	if md.Source != catalog.SourceOfflinePartial || md.Title != "" || md.Covers == nil || len(md.Covers) != 0 ||
		len(md.Warnings) != 1 || md.Warnings[0] != catalog.WarnUpstreamUnavailable || md.ISBN13 != foundationISBN {
		t.Fatalf("%+v", md)
	}
	if hits.Load() != 3 {
		t.Fatalf("want 1 try + 2 retries, got %d", hits.Load())
	}
	if len(cache.m) != 0 {
		t.Fatal("partial records must not be cached")
	}
}

func TestFetchISBNNotFoundReturnsPartial(t *testing.T) {
	ol, _ := fakeOpenLibrary(t, http.StatusOK)
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, nil)
	md, err := svc.FetchISBN(context.Background(), "9780441013593")
	if err != nil || md.Source != catalog.SourceOfflinePartial || md.Warnings[0] != catalog.WarnNotFound {
		t.Fatalf("%+v %v", md, err)
	}
}

func TestFetchISBNOfflineMode(t *testing.T) {
	svc := catalog.NewISBNService(fakeISBNRepo{}, olbooks.New("http://127.0.0.1:1", true), nil, logging.Discard())
	md, err := svc.FetchISBN(context.Background(), foundationISBN)
	if err != nil || md.Source != catalog.SourceOfflinePartial || md.Warnings[0] != catalog.WarnUpstreamUnavailable {
		t.Fatalf("%+v %v", md, err)
	}
}

// ── Identify tests (docs/03 §5.5) ────────────────────────────────────────────

func TestIdentifyFoundReturnsPatronSafeFields(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	cache := &memCache{}
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, cache)

	res, err := svc.Identify(context.Background(), "978-0-553-29335-7")
	if err != nil {
		t.Fatal(err)
	}
	if res.ISBN13 != foundationISBN || res.InCatalog || res.BookID != nil {
		t.Fatalf("header fields: %+v", res)
	}
	if res.Source != catalog.SourceOpenLibrary || len(res.Warnings) != 0 {
		t.Fatalf("source/warnings: %+v", res)
	}
	if res.Identified == nil {
		t.Fatal("Identified must not be nil on success")
	}
	id := res.Identified
	if id.Title != "Foundation" || len(id.Authors) != 1 || id.Authors[0] != "Isaac Asimov" {
		t.Fatalf("title/authors: %+v", id)
	}
	if id.Publisher == nil || *id.Publisher != "Bantam Spectra" {
		t.Fatalf("publisher: %v", id.Publisher)
	}
	if id.PublishedYear == nil || *id.PublishedYear != 1991 {
		t.Fatalf("year: %v", id.PublishedYear)
	}
	// CoverURL must be present; admin-only fields must be absent from result type.
	if id.CoverURL == nil || *id.CoverURL == "" {
		t.Fatalf("cover_url missing: %+v", id)
	}
	// Cache written for future hits.
	if len(cache.m) != 1 {
		t.Fatalf("cache not written: %v", cache.m)
	}
	_ = hits
}

func TestIdentifyCacheHitSkipsOpenLibrary(t *testing.T) {
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	cache := &memCache{}
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, cache)

	_, _ = svc.Identify(context.Background(), foundationISBN)
	before := hits.Load()
	res, err := svc.Identify(context.Background(), foundationISBN)
	if err != nil || res.Identified == nil || res.Identified.Title != "Foundation" {
		t.Fatalf("second call: %+v %v", res, err)
	}
	if hits.Load() != before {
		t.Fatalf("cache miss: %d→%d OL calls", before, hits.Load())
	}
}

func TestIdentifyInvalidISBN(t *testing.T) {
	svc := catalog.NewISBNService(fakeISBNRepo{}, olbooks.New("", false), nil, logging.Discard())
	_, err := svc.Identify(context.Background(), "9780553293358") // bad check digit
	var de *model.Error
	if !errors.As(err, &de) || de.Code != model.CodeInvalidISBN {
		t.Fatalf("want INVALID_ISBN, got %v", err)
	}
}

func TestIdentifyInCatalog(t *testing.T) {
	id := uuid.New()
	ol, hits := fakeOpenLibrary(t, http.StatusOK)
	svc := newISBN(t, ol.URL, fakeISBNRepo{byISBN: map[string]uuid.UUID{foundationISBN: id}}, nil)

	res, err := svc.Identify(context.Background(), foundationISBN)
	if err != nil {
		t.Fatal(err)
	}
	if !res.InCatalog || res.BookID == nil || *res.BookID != id.String() {
		t.Fatalf("in_catalog: %+v", res)
	}
	if res.Identified != nil {
		t.Fatalf("identified must be nil when in catalog: %+v", res.Identified)
	}
	if res.Source != catalog.SourceLocal || len(res.Warnings) != 0 {
		t.Fatalf("source/warnings: %+v", res)
	}
	if hits.Load() != 0 {
		t.Fatal("must not hit Open Library when already in catalog")
	}
}

func TestIdentifyNotFound(t *testing.T) {
	ol, _ := fakeOpenLibrary(t, http.StatusOK) // returns {} for unknown ISBNs
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, nil)

	res, err := svc.Identify(context.Background(), "9780441013593") // not in stub
	if err != nil {
		t.Fatal(err)
	}
	if res.InCatalog || res.BookID != nil || res.Identified != nil {
		t.Fatalf("unexpected hit: %+v", res)
	}
	if res.Source != catalog.SourceOfflinePartial || len(res.Warnings) != 1 || res.Warnings[0] != catalog.WarnNotFound {
		t.Fatalf("partial fields: %+v", res)
	}
}

func TestIdentifyUpstreamDown(t *testing.T) {
	ol, _ := fakeOpenLibrary(t, http.StatusInternalServerError)
	svc := newISBN(t, ol.URL, fakeISBNRepo{}, nil)

	res, err := svc.Identify(context.Background(), foundationISBN)
	if err != nil {
		t.Fatal(err)
	}
	if res.Identified != nil {
		t.Fatalf("Identified must be nil on upstream error: %+v", res.Identified)
	}
	if res.Source != catalog.SourceOfflinePartial || len(res.Warnings) != 1 || res.Warnings[0] != catalog.WarnUpstreamUnavailable {
		t.Fatalf("partial fields: %+v", res)
	}
}

func TestIdentifyOfflineMode(t *testing.T) {
	svc := catalog.NewISBNService(fakeISBNRepo{}, olbooks.New("http://127.0.0.1:1", true), nil, logging.Discard())
	res, err := svc.Identify(context.Background(), foundationISBN)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != catalog.SourceOfflinePartial || res.Warnings[0] != catalog.WarnUpstreamUnavailable {
		t.Fatalf("%+v", res)
	}
	if res.Identified != nil {
		t.Fatalf("identified must be nil in offline mode")
	}
}
