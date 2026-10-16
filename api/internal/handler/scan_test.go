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
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
)

type stubScanRepo struct{ *stubCatalogRepo }

func (s stubScanRepo) BookIDByISBN(_ context.Context, isbn string) (uuid.UUID, bool, error) {
	if isbn == "9780441013593" {
		return s.id, true, nil
	}
	return uuid.Nil, false, nil
}

func (s stubScanRepo) CopyRefByBarcode(context.Context, string) (uuid.UUID, uuid.UUID, bool, error) {
	return uuid.Nil, uuid.Nil, false, nil
}

type stubReturnRepo struct {
	known    uuid.UUID
	gotActor uuid.UUID
}

func (s *stubReturnRepo) ReturnCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	s.gotActor = actor
	switch id {
	case s.known:
		return model.ReturnData{
			Copy:  model.AdminCopy{ID: id, Barcode: "ATH-002941-C01", Status: model.CopyAvailable, Condition: "good", LastStateChange: now},
			Total: 3, Available: 3, Statuses: []string{model.CopyAvailable},
		}, nil
	case uuid.Nil:
		return model.ReturnData{}, model.New(model.CodeNotFound, "Copy not found.")
	default:
		return model.ReturnData{}, model.New(model.CodeConflict, "Copy is not checked out").
			WithExtra(map[string]any{"current_status": "available"})
	}
}

type scanFixture struct {
	h      http.Handler
	cookie *http.Cookie
	ret    *stubReturnRepo
	user   uuid.UUID
	mr     *miniredis.Miniredis
}

func scanApp(t *testing.T, withLimiter bool) scanFixture {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	ret := &stubReturnRepo{known: uuid.New()}
	d := server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Scan:        catalog.NewScanService(stubScanRepo{&stubCatalogRepo{id: uuid.New()}}),
		Circulation: circulation.New(ret),
	}
	var mr *miniredis.Miniredis
	if withLimiter {
		mr = miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		d.Limiter = middleware.NewRateLimiter(rdb, logging.Discard())
	}
	user := uuid.New()
	tok, _, _ := jm.Issue(jwt.Claims{Subject: user.String(), Role: "user"})
	return scanFixture{h: server.NewRouter(d), cookie: &http.Cookie{Name: middleware.AccessCookie, Value: tok}, ret: ret, user: user, mr: mr}
}

func post(h http.Handler, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestScanEndpoint(t *testing.T) {
	f := scanApp(t, false)
	if rec := post(f.h, "/api/books/scan", `{"code":"9780441013593"}`, nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}

	rec := post(f.h, "/api/books/scan", `{"code":"978-0441013593"}`, f.cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	for _, k := range []string{"code", "code_type", "in_catalog", "match", "book", "copy", "availability", "wayfinding", "returnable"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if len(body) != 9 {
		t.Errorf("unexpected keys: %v", body)
	}
	if body["code"] != "9780441013593" || body["code_type"] != "isbn13" || body["match"] != "book" || body["in_catalog"] != true ||
		body["copy"] != nil || body["returnable"] != false {
		t.Fatalf("body %v", body)
	}
	book := body["book"].(map[string]any)
	if book["title"] != "Dune" || len(book) != 6 {
		t.Fatalf("book %v", book)
	}
	if w := body["wayfinding"].(map[string]any); w["path"] != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("wayfinding %v", w)
	}

	rec = post(f.h, "/api/books/scan", `{"code":"ATH-000001-C01"}`, f.cookie)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 200 || body["in_catalog"] != false || body["match"] != "none" || body["book"] != nil || body["code_type"] != "copy_barcode" {
		t.Fatalf("no match: %d %s", rec.Code, rec.Body)
	}

	cases := map[string]struct {
		body   string
		status int
		code   string
	}{
		"bad checksum": {`{"code":"9780441013590"}`, 400, "INVALID_ISBN"},
		"garbage":      {`{"code":"hello"}`, 400, "INVALID_ISBN"},
		"empty":        {`{"code":"  "}`, 400, "VALIDATION_FAILED"},
		"missing":      {`{}`, 400, "VALIDATION_FAILED"},
		"not json":     {`nope`, 400, "VALIDATION_FAILED"},
	}
	for name, tc := range cases {
		rec := post(f.h, "/api/books/scan", tc.body, f.cookie)
		if rec.Code != tc.status || errCode(t, rec) != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestReturnEndpoint(t *testing.T) {
	f := scanApp(t, false)
	path := "/api/copies/" + f.ret.known.String() + "/return"
	if rec := post(f.h, path, "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec := post(f.h, path, "", f.cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.ret.gotActor != f.user {
		t.Fatalf("actor %v want %v", f.ret.gotActor, f.user)
	}
	var body struct {
		Copy         map[string]any `json:"copy"`
		Availability map[string]any `json:"availability"`
		Hold         any            `json:"hold"`
	}
	raw := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(raw) != 3 || body.Hold != nil || body.Copy["status"] != "available" || body.Availability["state"] != "available" {
		t.Fatalf("body %s", rec.Body)
	}
	for _, k := range []string{"id", "barcode", "status", "condition", "last_state_change", "location"} {
		if _, ok := body.Copy[k]; !ok {
			t.Errorf("copy missing %s", k)
		}
	}

	rec = post(f.h, "/api/copies/"+uuid.NewString()+"/return", "", f.cookie)
	var conflict map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &conflict)
	if rec.Code != 409 || conflict["error"] != "CONFLICT" || conflict["message"] != "Copy is not checked out" || conflict["current_status"] != "available" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		rec := post(f.h, "/api/copies/"+id+"/return", "", f.cookie)
		if rec.Code != 404 || errCode(t, rec) != "NOT_FOUND" {
			t.Errorf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
}

func TestScanAndReturnRateLimits(t *testing.T) {
	f := scanApp(t, true)
	for i := 1; i <= 30; i++ {
		if rec := post(f.h, "/api/books/scan", `{"code":"9780441013593"}`, f.cookie); rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "30" {
			t.Fatalf("scan %d: %d limit=%s", i, rec.Code, rec.Header().Get("X-RateLimit-Limit"))
		}
	}
	if rec := post(f.h, "/api/books/scan", `{"code":"9780441013593"}`, f.cookie); rec.Code != 429 || errCode(t, rec) != "RATE_LIMITED" {
		t.Fatalf("31st scan: %d %s", rec.Code, rec.Body)
	}

	path := "/api/copies/" + f.ret.known.String() + "/return"
	for i := 1; i <= 10; i++ {
		if rec := post(f.h, path, "", f.cookie); rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "10" {
			t.Fatalf("return %d: %d limit=%s", i, rec.Code, rec.Header().Get("X-RateLimit-Limit"))
		}
	}
	if rec := post(f.h, path, "", f.cookie); rec.Code != 429 || errCode(t, rec) != "RATE_LIMITED" {
		t.Fatalf("11th return: %d %s", rec.Code, rec.Body)
	}

	// Dedicated buckets are keyed per user and shadow the general bucket.
	if !f.mr.Exists("rl:books_scan:u:"+f.user.String()) || !f.mr.Exists("rl:copy_return:u:"+f.user.String()) {
		t.Fatalf("keys %v", f.mr.Keys())
	}
	for _, k := range f.mr.Keys() {
		if strings.HasPrefix(k, "rl:general") {
			t.Fatalf("general bucket applied: %s", k)
		}
	}
}
