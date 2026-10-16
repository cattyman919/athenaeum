package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
)

// stubAdminRepo implements only what the tests exercise; any other call panics on the
// nil embedded interface.
type stubAdminRepo struct {
	admin.Repository
	dupISBN  uuid.UUID
	gotPatch model.CopyPatch
	gotLimit int
}

func (s *stubAdminRepo) KPIs(_ context.Context, now time.Time) (model.AdminKPIs, error) {
	return model.AdminKPIs{TitlesInCatalog: 40, Copies: model.CopyCounts{Total: 60, Available: 48, CheckedOut: 12},
		ShelfCapacity: model.Occupancy{Occupied: 60, Total: 600}, ComputedAt: now}, nil
}

func (s *stubAdminRepo) CopyEvents(_ context.Context, _ *time.Time, limit int) ([]model.CopyEvent, error) {
	s.gotLimit = limit
	return []model.CopyEvent{}, nil
}

func (s *stubAdminRepo) BookIDByISBN(context.Context, string) (uuid.UUID, bool, error) {
	return s.dupISBN, s.dupISBN != uuid.Nil, nil
}

func (s *stubAdminRepo) PatchCopy(_ context.Context, _, id uuid.UUID, p model.CopyPatch, _ time.Time) (model.AdminCopy, *model.LocationRow, error) {
	s.gotPatch = p
	return model.AdminCopy{ID: id, Barcode: "ATH-002941-C01", Status: *p.Status}, nil, nil
}

func (s *stubAdminRepo) CopyIDByBarcode(context.Context, string) (uuid.UUID, error) {
	return uuid.New(), nil
}

type stubIndexStatus struct {
	gotLimit int
	st       model.IndexStatus
}

func (s *stubIndexStatus) PendingStatus(_ context.Context, limit int) (model.IndexStatus, error) {
	s.gotLimit = limit
	return s.st, nil
}

func adminApp(t *testing.T) (http.Handler, *stubAdminRepo, *http.Cookie, *http.Cookie, uuid.UUID) {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	repo := &stubAdminRepo{}
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Admin: admin.New(repo, nil, nil).WithIndexStatus(&stubIndexStatus{
			st: model.IndexStatus{PendingCount: 2, PendingBookIDs: []string{"a", "b"}},
		}),
	})
	adminID := uuid.New()
	at, _, _ := jm.Issue(jwt.Claims{Subject: adminID.String(), Role: "admin"})
	pt, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	return r, repo, &http.Cookie{Name: middleware.AccessCookie, Value: at},
		&http.Cookie{Name: middleware.AccessCookie, Value: pt}, adminID
}

func adminSend(r http.Handler, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func adminBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return body
}

var adminRoutes = []struct{ method, path string }{
	{"GET", "/api/admin/data"},
	{"GET", "/api/admin/index-status"},
	{"GET", "/api/admin/copy-events"},
	{"GET", "/api/admin/books"},
	{"POST", "/api/admin/books"},
	{"PATCH", "/api/admin/books/" + uuid.NewString()},
	{"DELETE", "/api/admin/books/" + uuid.NewString()},
	{"POST", "/api/admin/copies"},
	{"PATCH", "/api/admin/copies/" + uuid.NewString()},
	{"DELETE", "/api/admin/copies/" + uuid.NewString()},
	{"GET", "/api/admin/tree"},
	{"GET", "/api/admin/shelves/" + uuid.NewString()},
	{"GET", "/api/admin/users"},
	{"PATCH", "/api/admin/users/" + uuid.NewString()},
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	r, _, _, patron, _ := adminApp(t)
	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := adminSend(r, rt.method, rt.path, `{}`, patron)
			if rec.Code != http.StatusForbidden || adminBody(t, rec)["error"] != "FORBIDDEN" {
				t.Fatalf("patron: %d %s", rec.Code, rec.Body)
			}
			rec = adminSend(r, rt.method, rt.path, `{}`, nil)
			if rec.Code != http.StatusUnauthorized || adminBody(t, rec)["error"] != "UNAUTHENTICATED" {
				t.Fatalf("anonymous: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestAdminDataAndEvents(t *testing.T) {
	r, repo, adm, _, _ := adminApp(t)
	rec := adminSend(r, "GET", "/api/admin/data", "", adm)
	body := adminBody(t, rec)
	if rec.Code != 200 || body["titles_in_catalog"] != float64(40) || body["computed_at"] == nil {
		t.Fatalf("data %d %v", rec.Code, body)
	}
	copies, _ := body["copies"].(map[string]any)
	if copies["missing_lost"] != float64(0) || copies["checked_out"] != float64(12) {
		t.Fatalf("copies %v", copies)
	}

	rec = adminSend(r, "GET", "/api/admin/copy-events?since=2026-10-06T19:52:03Z", "", adm)
	if rec.Code != 200 || repo.gotLimit != 100 || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("events %d %s limit=%d", rec.Code, rec.Body, repo.gotLimit)
	}
	for _, q := range []string{"since=yesterday", "limit=0x", "limit=501"} {
		if rec := adminSend(r, "GET", "/api/admin/copy-events?"+q, "", adm); rec.Code != 400 {
			t.Fatalf("%s: %d", q, rec.Code)
		}
	}
}

func TestAdminIndexStatus(t *testing.T) {
	r, _, adm, _, _ := adminApp(t)
	rec := adminSend(r, "GET", "/api/admin/index-status", "", adm)
	if rec.Code != 200 || rec.Body.String() != `{"pending_count":2,"pending_book_ids":["a","b"]}` {
		t.Fatalf("index-status %d %s", rec.Code, rec.Body)
	}

	src := &stubIndexStatus{}
	svc := admin.New(&stubAdminRepo{}, nil, nil).WithIndexStatus(src)
	st, err := svc.IndexStatus(context.Background())
	if err != nil || src.gotLimit != admin.MaxIndexStatusIDs || st.PendingBookIDs == nil || st.PendingCount != 0 {
		t.Fatalf("empty backlog %+v %v limit=%d", st, err, src.gotLimit)
	}
	if _, err := admin.New(&stubAdminRepo{}, nil, nil).IndexStatus(context.Background()); err == nil {
		t.Fatal("unconfigured source must error")
	}
}

func TestAdminValidation(t *testing.T) {
	r, repo, adm, _, adminID := adminApp(t)
	existing := uuid.New()
	repo.dupISBN = existing

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"malformed body", "POST", "/api/admin/books", `{`, 400, "VALIDATION_FAILED"},
		{"missing title", "POST", "/api/admin/books", `{"authors":["A"]}`, 400, "VALIDATION_FAILED"},
		{"bad isbn", "POST", "/api/admin/books", `{"title":"X","isbn13":"9780441013590"}`, 400, "INVALID_ISBN"},
		{"bad copy status", "POST", "/api/admin/books", `{"title":"X","copies":[{"status":"lost"}]}`, 400, "VALIDATION_FAILED"},
		{"bad barcode", "POST", "/api/admin/books", `{"title":"X","copies":[{"barcode":"ATH-2941-C1"}]}`, 400, "VALIDATION_FAILED"},
		{"bad shelf path", "POST", "/api/admin/books", `{"title":"X","copies":[{"shelf_path":"MAIN/3/SF"}]}`, 400, "VALIDATION_FAILED"},
		{"isbn conflict", "POST", "/api/admin/books", `{"title":"Dune","isbn13":"9780441013593","section":"SF"}`, 409, "ISBN_CONFLICT"},
		{"book id unknown", "PATCH", "/api/admin/books/nope", `{"title":"X"}`, 404, "NOT_FOUND"},
		{"copy missing book", "POST", "/api/admin/copies", `{"status":"available"}`, 400, "VALIDATION_FAILED"},
		{"barcode immutable", "PATCH", "/api/admin/copies/" + uuid.NewString(), `{"barcode":"ATH-002941-C09"}`, 400, "VALIDATION_FAILED"},
		{"bad shelf id", "PATCH", "/api/admin/copies/" + uuid.NewString(), `{"shelf_id":"S-2"}`, 400, "VALIDATION_FAILED"},
		{"bad copy patch status", "PATCH", "/api/admin/copies/" + uuid.NewString(), `{"status":"gone"}`, 400, "VALIDATION_FAILED"},
		{"shelf id unknown", "GET", "/api/admin/shelves/nope", ``, 404, "NOT_FOUND"},
		{"per_page too big", "GET", "/api/admin/users?per_page=51", ``, 400, "VALIDATION_FAILED"},
		{"bad role", "PATCH", "/api/admin/users/" + uuid.NewString(), `{"role":"root"}`, 400, "VALIDATION_FAILED"},
		{"self demote", "PATCH", "/api/admin/users/" + adminID.String(), `{"role":"user"}`, 400, "VALIDATION_FAILED"},
		{"bad sort", "GET", "/api/admin/books?sort=isbn:asc", ``, 400, "VALIDATION_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminSend(r, tt.method, tt.path, tt.body, adm)
			body := adminBody(t, rec)
			if rec.Code != tt.status || body["error"] != tt.code {
				t.Fatalf("%d %v", rec.Code, body)
			}
			if tt.code == "ISBN_CONFLICT" && body["book_id"] != existing.String() {
				t.Fatalf("book_id %v", body["book_id"])
			}
		})
	}
}

func TestAdminPatchCopyByBarcode(t *testing.T) {
	r, repo, adm, _, _ := adminApp(t)
	rec := adminSend(r, "PATCH", "/api/admin/copies/ATH-002941-C01", `{"status":"available","condition":"fair","shelf_id":null}`, adm)
	if rec.Code != 200 || adminBody(t, rec)["status"] != "available" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if repo.gotPatch.Condition == nil || *repo.gotPatch.Condition != "fair" || !repo.gotPatch.ShelfID.Null {
		t.Fatalf("patch %+v", repo.gotPatch)
	}
}
