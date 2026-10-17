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

// stubCircRepo implements circulation.Repository for handler tests.
type stubCircRepo struct {
	knownReturn uuid.UUID
	knownBorrow uuid.UUID
	knownRenew  uuid.UUID
	gotActor    uuid.UUID
	loanItems   []model.LoanItem
}

func (s *stubCircRepo) ReturnCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	s.gotActor = actor
	switch id {
	case s.knownReturn:
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

func (s *stubCircRepo) BorrowCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.BorrowData, error) {
	s.gotActor = actor
	switch id {
	case s.knownBorrow:
		loanID := uuid.New()
		dueAt := now.Add(model.LoanPeriod)
		return model.BorrowData{
			Loan: model.LoanDetail{
				ID: loanID, CopyID: id, UserID: actor, BorrowedAt: now, DueAt: dueAt,
				Renewals: 0, Renewable: true,
			},
			Copy:  model.AdminCopy{ID: id, Barcode: "ATH-002941-C02", Status: model.CopyCheckedOut, Condition: "good", LastStateChange: now},
			Total: 3, Available: 1, Statuses: []string{model.CopyAvailable, model.CopyCheckedOut},
		}, nil
	case uuid.Nil:
		return model.BorrowData{}, model.New(model.CodeNotFound, "Copy not found.")
	default:
		return model.BorrowData{}, model.New(model.CodeConflict, "Copy is not available to borrow").
			WithExtra(map[string]any{"current_status": "checked_out"})
	}
}

func (s *stubCircRepo) RenewLoan(_ context.Context, actor, id uuid.UUID, now time.Time) (model.RenewData, error) {
	s.gotActor = actor
	switch id {
	case s.knownRenew:
		return model.RenewData{ID: uuid.New(), DueAt: now.Add(model.LoanPeriod), Renewals: 1}, nil
	case uuid.Nil:
		return model.RenewData{}, model.New(model.CodeNotFound, "Copy not found.")
	default:
		return model.RenewData{}, model.New(model.CodeConflict, "No open loan on this copy")
	}
}

func (s *stubCircRepo) ListLoans(_ context.Context, _ uuid.UUID, _ time.Time) ([]model.LoanItem, model.LoansMeta, error) {
	items := s.loanItems
	if items == nil {
		items = []model.LoanItem{}
	}
	return items, model.LoansMeta{Active: len(items)}, nil
}

func (s *stubCircRepo) OpenLoanByCopy(_ context.Context, _, _ uuid.UUID) (*model.ScanLoanRef, error) {
	return nil, nil
}

func (s *stubCircRepo) CountOpenLoansByUser(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (s *stubCircRepo) BorrowCopyForBook(_ context.Context, actor, id uuid.UUID, now time.Time) (model.BorrowData, error) {
	s.gotActor = actor
	switch id {
	case s.knownBorrow:
		loanID := uuid.New()
		dueAt := now.Add(model.LoanPeriod)
		return model.BorrowData{
			Loan:  model.LoanDetail{ID: loanID, CopyID: uuid.New(), UserID: actor, BorrowedAt: now, DueAt: dueAt, Renewals: 0, Renewable: true},
			Copy:  model.AdminCopy{ID: uuid.New(), Barcode: "ATH-002941-C01", Status: model.CopyCheckedOut, Condition: "good", LastStateChange: now},
			Total: 2, Available: 1, Statuses: []string{model.CopyAvailable, model.CopyCheckedOut},
		}, nil
	case uuid.Nil:
		return model.BorrowData{}, model.New(model.CodeNotFound, "Book not found.")
	default:
		return model.BorrowData{}, model.New(model.CodeConflict, "No copy available to borrow for this book.")
	}
}

func (s *stubCircRepo) ReturnCopyForBook(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	s.gotActor = actor
	switch id {
	case s.knownReturn:
		return model.ReturnData{
			Copy:  model.AdminCopy{ID: uuid.New(), Barcode: "ATH-002941-C01", Status: model.CopyAvailable, Condition: "good", LastStateChange: now},
			Total: 2, Available: 2, Statuses: []string{model.CopyAvailable},
		}, nil
	case uuid.Nil:
		return model.ReturnData{}, model.New(model.CodeNotFound, "Book not found.")
	default:
		return model.ReturnData{}, model.New(model.CodeConflict, "You have no open loan for this book.")
	}
}

func (s *stubCircRepo) BookBorrowInfo(_ context.Context, _, _ uuid.UUID) (bool, *model.ScanBorrowCopy, bool, []model.ScanMyLoan, error) {
	return false, nil, false, []model.ScanMyLoan{}, nil
}

type scanFixture struct {
	h      http.Handler
	cookie *http.Cookie
	circ   *stubCircRepo
	user   uuid.UUID
	mr     *miniredis.Miniredis
}

func scanApp(t *testing.T, withLimiter bool) scanFixture {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	circ := &stubCircRepo{knownReturn: uuid.New(), knownBorrow: uuid.New(), knownRenew: uuid.New()}
	d := server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Scan:        catalog.NewScanService(stubScanRepo{&stubCatalogRepo{id: uuid.New()}}),
		Circulation: circulation.New(circ),
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
	return scanFixture{h: server.NewRouter(d), cookie: &http.Cookie{Name: middleware.AccessCookie, Value: tok}, circ: circ, user: user, mr: mr}
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

func getReq(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ── /api/books/scan ───────────────────────────────────────────────────────────

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
	// Now 13 keys: 9 original + borrowable + loan + borrow_copy + my_loans (plan M21).
	for _, k := range []string{"code", "code_type", "in_catalog", "match", "book", "copy",
		"availability", "wayfinding", "returnable", "borrowable", "loan", "borrow_copy", "my_loans"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing key %q in scan response", k)
		}
	}
	if len(body) != 13 {
		t.Errorf("unexpected keys (%d): %v", len(body), body)
	}
	if body["code"] != "9780441013593" || body["code_type"] != "isbn13" || body["match"] != "book" || body["in_catalog"] != true ||
		body["copy"] != nil || body["returnable"] != false || body["borrowable"] != false || body["loan"] != nil ||
		body["borrow_copy"] != nil {
		t.Fatalf("body %v", body)
	}
	if loans, ok := body["my_loans"].([]any); !ok || len(loans) != 0 {
		t.Fatalf("my_loans: %v", body["my_loans"])
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

// ── /api/copies/{id}/return ───────────────────────────────────────────────────

func TestReturnEndpoint(t *testing.T) {
	f := scanApp(t, false)
	path := "/api/copies/" + f.circ.knownReturn.String() + "/return"
	if rec := post(f.h, path, "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec := post(f.h, path, "", f.cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.circ.gotActor != f.user {
		t.Fatalf("actor %v want %v", f.circ.gotActor, f.user)
	}
	var body struct {
		Copy         map[string]any `json:"copy"`
		Availability map[string]any `json:"availability"`
		Hold         any            `json:"hold"`
		ClosedLoan   any            `json:"closed_loan"`
	}
	raw := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	// Response has 4 keys: copy, availability, hold, closed_loan.
	if len(raw) != 4 || body.Hold != nil || body.ClosedLoan != nil ||
		body.Copy["status"] != "available" || body.Availability["state"] != "available" {
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

// ── /api/copies/{id}/borrow ───────────────────────────────────────────────────

func TestBorrowEndpoint(t *testing.T) {
	f := scanApp(t, false)
	path := "/api/copies/" + f.circ.knownBorrow.String() + "/borrow"

	if rec := post(f.h, path, "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec := post(f.h, path, "", f.cookie)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body struct {
		Loan         map[string]any `json:"loan"`
		Copy         map[string]any `json:"copy"`
		Availability map[string]any `json:"availability"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Loan == nil || body.Copy == nil || body.Availability == nil {
		t.Fatalf("missing top-level key: %s", rec.Body)
	}
	for _, k := range []string{"id", "copy_id", "user_id", "borrowed_at", "due_at", "returned_at", "renewals", "renewable"} {
		if _, ok := body.Loan[k]; !ok {
			t.Errorf("loan missing %q", k)
		}
	}
	if body.Copy["status"] != "checked_out" {
		t.Fatalf("copy status %v", body.Copy["status"])
	}

	// Conflict: copy unavailable.
	rec = post(f.h, "/api/copies/"+uuid.NewString()+"/borrow", "", f.cookie)
	if rec.Code != 409 || errCode(t, rec) != "CONFLICT" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
	// Not found.
	rec = post(f.h, "/api/copies/00000000-0000-0000-0000-000000000000/borrow", "", f.cookie)
	if rec.Code != 404 || errCode(t, rec) != "NOT_FOUND" {
		t.Fatalf("not found: %d %s", rec.Code, rec.Body)
	}
}

// ── /api/copies/{id}/renew ────────────────────────────────────────────────────

func TestRenewEndpoint(t *testing.T) {
	f := scanApp(t, false)
	path := "/api/copies/" + f.circ.knownRenew.String() + "/renew"

	if rec := post(f.h, path, "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec := post(f.h, path, "", f.cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body struct {
		Loan map[string]any `json:"loan"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Loan == nil {
		t.Fatalf("loan missing: %s", rec.Body)
	}
	for _, k := range []string{"id", "due_at", "renewals", "renewable"} {
		if _, ok := body.Loan[k]; !ok {
			t.Errorf("loan missing %q", k)
		}
	}
	if body.Loan["renewals"].(float64) != 1 || body.Loan["renewable"] != false {
		t.Fatalf("renewal values: %v", body.Loan)
	}

	// Conflict: no open loan.
	rec = post(f.h, "/api/copies/"+uuid.NewString()+"/renew", "", f.cookie)
	if rec.Code != 409 || errCode(t, rec) != "CONFLICT" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
}

// ── GET /api/loans ────────────────────────────────────────────────────────────

func TestListLoansEndpoint(t *testing.T) {
	f := scanApp(t, false)
	path := "/api/loans"

	if rec := getReq(f.h, path, nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec := getReq(f.h, path, f.cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body struct {
		Items []any          `json:"items"`
		Meta  map[string]any `json:"meta"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Items == nil || body.Meta == nil {
		t.Fatalf("bad envelope: %s", rec.Body)
	}
	if _, ok := body.Meta["active"]; !ok {
		t.Errorf("meta missing active")
	}
	if _, ok := body.Meta["overdue"]; !ok {
		t.Errorf("meta missing overdue")
	}
}

// ── POST /api/books/{id}/borrow ───────────────────────────────────────────────

func TestBorrowByBookEndpoint(t *testing.T) {
	f := scanApp(t, false)

	// Unauthenticated → 401.
	if rec := post(f.h, "/api/books/"+f.circ.knownBorrow.String()+"/borrow", "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}

	// Known book → 201 with loan + copy + availability.
	rec := post(f.h, "/api/books/"+f.circ.knownBorrow.String()+"/borrow", "", f.cookie)
	if rec.Code != 201 {
		t.Fatalf("201 expected: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Loan         map[string]any `json:"loan"`
		Copy         map[string]any `json:"copy"`
		Availability map[string]any `json:"availability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Loan == nil || body.Copy == nil || body.Availability == nil {
		t.Fatalf("envelope: %s", rec.Body)
	}
	for _, k := range []string{"id", "copy_id", "user_id", "borrowed_at", "due_at", "renewals", "renewable"} {
		if _, ok := body.Loan[k]; !ok {
			t.Errorf("loan missing %q", k)
		}
	}

	// No copy available → 409 CONFLICT.
	rec = post(f.h, "/api/books/"+uuid.NewString()+"/borrow", "", f.cookie)
	if rec.Code != 409 || errCode(t, rec) != "CONFLICT" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
}

// ── POST /api/books/{id}/return ───────────────────────────────────────────────

func TestReturnByBookEndpoint(t *testing.T) {
	f := scanApp(t, false)

	// Unauthenticated → 401.
	if rec := post(f.h, "/api/books/"+f.circ.knownReturn.String()+"/return", "", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}

	// Known book → 200 with copy + availability.
	rec := post(f.h, "/api/books/"+f.circ.knownReturn.String()+"/return", "", f.cookie)
	if rec.Code != 200 {
		t.Fatalf("200 expected: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Copy         map[string]any `json:"copy"`
		Availability map[string]any `json:"availability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Copy == nil || body.Availability == nil {
		t.Fatalf("envelope: %s", rec.Body)
	}

	// No loan → 409 CONFLICT.
	rec = post(f.h, "/api/books/"+uuid.NewString()+"/return", "", f.cookie)
	if rec.Code != 409 || errCode(t, rec) != "CONFLICT" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
}

// ── Rate limits ───────────────────────────────────────────────────────────────

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

	retPath := "/api/copies/" + f.circ.knownReturn.String() + "/return"
	for i := 1; i <= 10; i++ {
		if rec := post(f.h, retPath, "", f.cookie); rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "10" {
			t.Fatalf("return %d: %d limit=%s", i, rec.Code, rec.Header().Get("X-RateLimit-Limit"))
		}
	}
	if rec := post(f.h, retPath, "", f.cookie); rec.Code != 429 || errCode(t, rec) != "RATE_LIMITED" {
		t.Fatalf("11th return: %d %s", rec.Code, rec.Body)
	}

	// Borrow: 10/min.
	bPath := "/api/copies/" + f.circ.knownBorrow.String() + "/borrow"
	if rec := post(f.h, bPath, "", f.cookie); rec.Code != 201 || rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("borrow 1: %d limit=%s", rec.Code, rec.Header().Get("X-RateLimit-Limit"))
	}

	// Renew: 5/min.
	rPath := "/api/copies/" + f.circ.knownRenew.String() + "/renew"
	if rec := post(f.h, rPath, "", f.cookie); rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "5" {
		t.Fatalf("renew 1: %d limit=%s", rec.Code, rec.Header().Get("X-RateLimit-Limit"))
	}

	// Dedicated buckets are keyed per user and shadow the general bucket.
	if !f.mr.Exists("rl:books_scan:u:"+f.user.String()) || !f.mr.Exists("rl:copy_return:u:"+f.user.String()) {
		t.Fatalf("keys %v", f.mr.Keys())
	}
	if !f.mr.Exists("rl:copy_borrow:u:"+f.user.String()) || !f.mr.Exists("rl:copy_renew:u:"+f.user.String()) {
		t.Fatalf("borrow/renew keys missing in %v", f.mr.Keys())
	}
	// Book borrow/return reuse copy_borrow/copy_return buckets (plan M21);
	// verify the X-RateLimit-Limit header regardless of whether the bucket
	// is already exhausted (it may be after the 10 copy-return calls above).
	bbPath := "/api/books/" + f.circ.knownBorrow.String() + "/borrow"
	if rec := post(f.h, bbPath, "", f.cookie); rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("book borrow limit header: %d hdr=%s", rec.Code, rec.Header().Get("X-RateLimit-Limit"))
	}
	brPath := "/api/books/" + f.circ.knownReturn.String() + "/return"
	if rec := post(f.h, brPath, "", f.cookie); rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("book return limit header: %d hdr=%s", rec.Code, rec.Header().Get("X-RateLimit-Limit"))
	}
	for _, k := range f.mr.Keys() {
		if strings.HasPrefix(k, "rl:general") {
			t.Fatalf("general bucket applied: %s", k)
		}
	}
}
