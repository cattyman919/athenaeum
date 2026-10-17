package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
	"github.com/cattyman919/athenaeum/api/internal/service/waitlist"
)

type isbnRepo map[string]uuid.UUID

func (r isbnRepo) BookIDByISBN(_ context.Context, isbn string) (uuid.UUID, bool, error) {
	id, ok := r[isbn]
	return id, ok, nil
}

func (r isbnRepo) SimilarBooks(_ context.Context, _ string, _ []string) ([]model.SimilarBook, error) {
	return []model.SimilarBook{}, nil
}

type isbnApp struct {
	h       http.Handler
	admin   *http.Cookie
	user    *http.Cookie
	mr      *miniredis.Miniredis
	olCalls int
}

func newISBNApp(t *testing.T, repo isbnRepo) *isbnApp {
	t.Helper()
	app := &isbnApp{}
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.olCalls++
		if r.URL.Query().Get("bibkeys") == "ISBN:9780553293357" {
			_, _ = w.Write([]byte(`{"ISBN:9780553293357":{"title":"Foundation","authors":[{"name":"Isaac Asimov"}],"publish_date":"1951"}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(ol.Close)
	app.mr = miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: app.mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	svc := catalog.NewISBNService(repo, olbooks.New(ol.URL, false).WithRetry(0, 0),
		repository.NewISBNCache(rdb), logging.Discard())
	app.h = server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm, ISBN: svc,
		Limiter: middleware.NewRateLimiter(rdb, logging.Discard()),
	})
	at, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "admin"})
	ut, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	app.admin = &http.Cookie{Name: middleware.AccessCookie, Value: at}
	app.user = &http.Cookie{Name: middleware.AccessCookie, Value: ut}
	return app
}

func (a *isbnApp) post(cookie *http.Cookie, body string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/api/books/fetch-isbn", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestFetchISBNAuthAndRole(t *testing.T) {
	app := newISBNApp(t, nil)
	if rec, _ := app.post(nil, `{"isbn":"9780553293357"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rec.Code)
	}
	if rec, out := app.post(app.user, `{"isbn":"9780553293357"}`); rec.Code != http.StatusForbidden || out["error"] != "FORBIDDEN" {
		t.Fatalf("user: %d %v", rec.Code, out)
	}
}

func TestFetchISBNSuccessAndCache(t *testing.T) {
	app := newISBNApp(t, nil)
	rec, out := app.post(app.admin, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusOK || out["title"] != "Foundation" || out["source"] != "openlibrary" ||
		out["in_catalog"] != false || out["published_year"] != float64(1951) || out["publisher"] != nil {
		t.Fatalf("%d %v", rec.Code, out)
	}
	// similar must always be present (empty or not).
	if _, ok := out["similar"]; !ok {
		t.Fatalf("similar key missing from fetch-isbn response")
	}
	if rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("fetch_isbn bucket not applied: %v", rec.Header())
	}
	if !app.mr.Exists("isbn:9780553293357") {
		t.Fatal("redis cache not written")
	}
	if rec, _ := app.post(app.admin, `{"isbn":"9780553293357"}`); rec.Code != http.StatusOK || app.olCalls != 1 {
		t.Fatalf("second call: %d, open library calls %d", rec.Code, app.olCalls)
	}
}

func TestFetchISBNErrors(t *testing.T) {
	id := uuid.New()
	app := newISBNApp(t, isbnRepo{"9780441013593": id})
	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`{"isbn":"9780441013594"}`, 400, "INVALID_ISBN"},
		{`{"isbn":"036000291452"}`, 400, "INVALID_ISBN"},
		{`{}`, 400, "VALIDATION_FAILED"},
		{`not json`, 400, "VALIDATION_FAILED"},
		{`{"isbn":"9780441013593"}`, 409, "ISBN_CONFLICT"},
	}
	for _, c := range cases {
		rec, out := app.post(app.admin, c.body)
		if rec.Code != c.status || out["error"] != c.code {
			t.Errorf("%s: %d %v", c.body, rec.Code, out)
		}
		if c.code == "ISBN_CONFLICT" && out["book_id"] != id.String() {
			t.Errorf("conflict body %v", out)
		}
	}
	// Not in Open Library → 200 partial, intake never blocks.
	rec, out := app.post(app.admin, `{"isbn":"9780060850524"}`)
	if rec.Code != 200 || out["source"] != "offline_partial" || out["title"] != "" {
		t.Fatalf("%d %v", rec.Code, out)
	}
}

func TestFetchISBNRateLimitedAfterTen(t *testing.T) {
	app := newISBNApp(t, nil)
	for i := 1; i <= 10; i++ {
		if rec, _ := app.post(app.admin, `{"isbn":"9780441013594"}`); rec.Code != 400 {
			t.Fatalf("call %d: %d", i, rec.Code)
		}
	}
	rec, out := app.post(app.admin, `{"isbn":"9780553293357"}`)
	if rec.Code != 429 || out["error"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("11th: %d %v", rec.Code, out)
	}
}

// fetch-isbn coexists with /api/books/:id and /api/books/:id/waitlist routes.
func TestFetchISBNRouteCoexistsWithBookRoutes(t *testing.T) {
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked: %v", r)
		}
	}()
	_ = server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Catalog:  catalog.New(nil, nil),
		Library:  library.New(nil, nil),
		Waitlist: waitlist.New(nil, nil),
		ISBN:     catalog.NewISBNService(isbnRepo{}, olbooks.New("", true), nil, logging.Discard()),
	})
}

// ── Identify tests (docs/03 §5.5) ────────────────────────────────────────────

func (a *isbnApp) identify(cookie *http.Cookie, body string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/api/books/identify-isbn", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestIdentifyAuthRequiredAnyRole(t *testing.T) {
	app := newISBNApp(t, nil)
	// Unauthenticated → 401.
	if rec, _ := app.identify(nil, `{"isbn":"9780553293357"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rec.Code)
	}
	// Regular user is allowed (unlike fetch-isbn which requires admin).
	rec, out := app.identify(app.user, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusOK || out["isbn13"] != "9780553293357" {
		t.Fatalf("user: %d %v", rec.Code, out)
	}
	// Admin is also allowed.
	rec, out = app.identify(app.admin, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusOK || out["isbn13"] != "9780553293357" {
		t.Fatalf("admin: %d %v", rec.Code, out)
	}
}

func TestIdentifyEnvelopeShape(t *testing.T) {
	app := newISBNApp(t, nil)
	rec, out := app.identify(app.user, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, out)
	}
	// Top-level keys.
	for _, k := range []string{"isbn13", "in_catalog", "book_id", "identified", "source", "warnings"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if out["isbn13"] != "9780553293357" || out["in_catalog"] != false || out["book_id"] != nil {
		t.Fatalf("header fields: %v", out)
	}
	if out["source"] != "openlibrary" {
		t.Fatalf("source: %v", out["source"])
	}
	// warnings must be [] not null.
	w, ok := out["warnings"].([]any)
	if !ok || len(w) != 0 {
		t.Fatalf("warnings: %v", out["warnings"])
	}
	// identified sub-object: patron-safe fields only.
	id, ok := out["identified"].(map[string]any)
	if !ok {
		t.Fatalf("identified is not object: %v", out["identified"])
	}
	for _, k := range []string{"title", "authors", "publisher", "published_year", "cover_url"} {
		if _, ok := id[k]; !ok {
			t.Errorf("identified missing key %s", k)
		}
	}
	// Admin-only fields must not be present in identified.
	for _, k := range []string{"dewey_num", "loc_num", "description", "open_library_key"} {
		if _, ok := id[k]; ok {
			t.Errorf("identified must not expose admin field %s", k)
		}
	}
	if id["title"] != "Foundation" {
		t.Fatalf("title: %v", id["title"])
	}
}

func TestIdentifyInCatalogEnvelope(t *testing.T) {
	id := uuid.New()
	app := newISBNApp(t, isbnRepo{"9780553293357": id})
	rec, out := app.identify(app.user, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if out["in_catalog"] != true || out["book_id"] != id.String() {
		t.Fatalf("in_catalog: %v", out)
	}
	if out["identified"] != nil {
		t.Fatalf("identified must be null when in_catalog: %v", out["identified"])
	}
	if out["source"] != "local" {
		t.Fatalf("source: %v", out["source"])
	}
	w, _ := out["warnings"].([]any)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
}

func TestIdentifyValidationErrors(t *testing.T) {
	app := newISBNApp(t, nil)
	cases := []struct {
		body   string
		code   string
		status int
	}{
		{`{"isbn":"9780553293358"}`, "INVALID_ISBN", 400}, // bad check digit
		{`{}`, "VALIDATION_FAILED", 400},                  // missing isbn
		{`not json`, "VALIDATION_FAILED", 400},            // not JSON
	}
	for _, c := range cases {
		rec, out := app.identify(app.user, c.body)
		if rec.Code != c.status || out["error"] != c.code {
			t.Errorf("%s → %d %v", c.body, rec.Code, out)
		}
	}
}

func TestIdentifyPartialEnvelope(t *testing.T) {
	// ISBN not in OL stub → offline_partial + not_found warning.
	app := newISBNApp(t, nil)
	rec, out := app.identify(app.user, `{"isbn":"9780441013593"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if out["source"] != "offline_partial" {
		t.Fatalf("source: %v", out["source"])
	}
	if out["identified"] != nil {
		t.Fatalf("identified must be null: %v", out["identified"])
	}
	w, _ := out["warnings"].([]any)
	if len(w) != 1 || w[0] != "not_found" {
		t.Fatalf("warnings: %v", w)
	}
}

func TestIdentifyRateLimitBucketPresent(t *testing.T) {
	app := newISBNApp(t, nil)
	rec, _ := app.identify(app.user, `{"isbn":"9780553293357"}`)
	if rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("identify_isbn bucket not applied: %v", rec.Header())
	}
}

func TestIdentifyRateLimitedAfterTen(t *testing.T) {
	app := newISBNApp(t, nil)
	// Exhaust the 10/min bucket with invalid-checksum calls (fast, no OL).
	for i := 1; i <= 10; i++ {
		rec, _ := app.identify(app.user, `{"isbn":"9780553293358"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("call %d: %d", i, rec.Code)
		}
	}
	rec, out := app.identify(app.user, `{"isbn":"9780553293357"}`)
	if rec.Code != http.StatusTooManyRequests || out["error"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("11th: %d %v", rec.Code, out)
	}
}

func TestIdentifyRateLimitShadowsGeneral(t *testing.T) {
	app := newISBNApp(t, nil)
	// One call is enough to see the bucket key in Redis.
	app.identify(app.user, `{"isbn":"9780553293357"}`)
	// The identify_isbn bucket key must exist; general must not.
	found := false
	for _, k := range app.mr.Keys() {
		if strings.Contains(k, "identify_isbn") {
			found = true
		}
		if strings.HasPrefix(k, "rl:general") {
			t.Fatalf("general bucket applied to identify-isbn: %s", k)
		}
	}
	if !found {
		t.Fatalf("identify_isbn bucket key not found in Redis: %v", app.mr.Keys())
	}
}
