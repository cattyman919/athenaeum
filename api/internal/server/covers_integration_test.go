package server_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/covers"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

var coverJPEG = append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{7}, 3000)...)

// Seeded catalog → cover route read-through → versioned cover_url → 304, plus the
// seed warm-up against the same fake covers.openlibrary.org (docs/03 §5.6, docs/08 §6a).
func TestCoverCacheJourney(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fx, err := seed.Load("../../mocks")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Run(ctx, repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(coverJPEG)
	}))
	defer upstream.Close()

	coverRepo := repository.NewCoverRepository(pool)
	coverSvc := covers.New(coverRepo, covers.NewHTTPFetcher(upstream.URL, false), logging.Discard())
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	catalogSvc := catalog.New(repository.NewCatalogRepository(pool), nil)
	adminSvc := admin.New(repository.NewAdminRepository(pool), catalogSvc, repository.NewBuildingRepository(pool))
	a := &api{t: t, pool: pool, h: server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Auth:    auth.New(repository.NewUserRepository(pool), repository.NewTokenRepository(pool), jm, nil, time.Hour, logging.Discard()),
		Catalog: catalogSvc, Covers: coverSvc,
	})}
	a.register("covers@athenaeum.dev")

	coverOf := func() string {
		t.Helper()
		_, out := a.do("GET", "/api/books/search?q=dune", "")
		return out["items"].([]any)[0].(map[string]any)["cover_url"].(string)
	}
	dune := a.bookID("dune")

	// Before caching: the route without a version.
	if got := coverOf(); got != "/api/books/"+dune+"/cover" {
		t.Fatalf("uncached cover_url = %q", got)
	}

	// Anonymous <img> load: read-through fetch, stored, served with caching headers.
	anon := &api{t: t, h: a.h}
	rec, _ := anon.do("GET", "/api/books/"+dune+"/cover", "")
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), coverJPEG) || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || hits.Load() != 1 {
		t.Fatalf("cover: %d type=%q cc=%q hits=%d", rec.Code, rec.Header().Get("Content-Type"),
			rec.Header().Get("Cache-Control"), hits.Load())
	}
	etag := rec.Header().Get("ETag")

	// Cached: payloads carry the versioned URL; no more upstream traffic.
	if got := coverOf(); !regexp.MustCompile(`^/api/books/` + dune + `/cover\?v=[0-9a-f]{12}$`).MatchString(got) {
		t.Fatalf("cached cover_url = %q", got)
	}
	if rec, _ := anon.do("GET", "/api/books/"+dune+"/cover?v=whatever", ""); rec.Code != 200 || hits.Load() != 1 {
		t.Fatalf("cached get: %d hits=%d", rec.Code, hits.Load())
	}
	req := httptest.NewRequest("GET", "/api/books/"+dune+"/cover", nil)
	req.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	a.h.ServeHTTP(rec304, req)
	if rec304.Code != http.StatusNotModified || rec304.Body.Len() != 0 {
		t.Fatalf("conditional: %d", rec304.Code)
	}

	// Detail payload uses the same URL; admin keeps the raw upstream URL for editing.
	_, d := a.do("GET", "/api/books/"+dune, "")
	if d["cover_url"] != coverOf() {
		t.Fatalf("detail cover_url = %v", d["cover_url"])
	}
	ab, err := adminSvc.Book(ctx, uuid.MustParse(dune))
	if err != nil || ab.CoverURL == nil || !strings.HasPrefix(*ab.CoverURL, "https://covers.openlibrary.org/") {
		t.Fatalf("admin cover_url = %v (%v)", ab.CoverURL, err)
	}

	// Unknown / malformed ids → NOT_FOUND envelope.
	for _, p := range []string{"/api/books/" + uuid.NewString() + "/cover", "/api/books/nope/cover"} {
		if rec, e := anon.do("GET", p, ""); rec.Code != 404 || e["error"] != "NOT_FOUND" {
			t.Fatalf("%s: %d %v", p, rec.Code, e)
		}
	}

	// Warm-up fetches everything still uncached; a second run has nothing to do.
	before, err := coverRepo.Uncached(ctx)
	if err != nil || len(before) == 0 {
		t.Fatalf("uncached before warm: %d %v", len(before), err)
	}
	res, err := coverSvc.Warm(ctx, 4)
	if err != nil || res.Fetched != len(before) || res.Failed != 0 {
		t.Fatalf("warm: %+v %v (want %d)", res, err, len(before))
	}
	if after, _ := coverRepo.Uncached(ctx); len(after) != 0 {
		t.Fatalf("uncached after warm: %v", after)
	}

	// Changing a book's cover_url orphans the old bytes: the route is unversioned
	// again and Prune drops the stale row.
	if _, err := pool.Exec(ctx, `UPDATE books SET cover_url = 'https://covers.openlibrary.org/b/id/1-L.jpg' WHERE id = $1`, dune); err != nil {
		t.Fatal(err)
	}
	if got := coverOf(); got != "/api/books/"+dune+"/cover" {
		t.Fatalf("after cover change cover_url = %q", got)
	}
	if n, err := coverRepo.Prune(ctx); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
}
