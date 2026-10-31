package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/service/waitlist"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

type api struct {
	t       *testing.T
	h       http.Handler
	pool    *pgxpool.Pool
	cookies []*http.Cookie
}

func (a *api) do(method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range a.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func newAPI(t *testing.T) *api {
	t.Helper()
	pool := pgtest.New(t)
	fx, err := seed.Load("../../mocks")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Run(context.Background(), repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	authSvc := auth.New(repository.NewUserRepository(pool), repository.NewTokenRepository(pool), jm, nil, time.Hour, logging.Discard())
	libRepo, waitRepo := repository.NewLibraryRepository(pool), repository.NewWaitlistRepository(pool)
	indexStore := repository.NewIndexStore(pool)
	catalogSvc := catalog.New(repository.NewCatalogRepository(pool), nil).WithPendingCounter(indexStore)
	h := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm, Auth: authSvc,
		Catalog: catalogSvc,
		Admin: admin.New(repository.NewAdminRepository(pool), catalogSvc, repository.NewBuildingRepository(pool)).
			WithIndexStatus(indexStore),
		Library:     library.New(libRepo, waitRepo),
		Waitlist:    waitlist.New(waitRepo, libRepo),
		Scan:        catalog.NewScanService(repository.NewCatalogRepository(pool)),
		Circulation: circulation.New(repository.NewCirculationRepository(pool)),
	})
	return &api{t: t, h: h, pool: pool}
}

func (a *api) register(email string) {
	a.t.Helper()
	rec, _ := a.do("POST", "/api/auth/register", `{"name":"T","email":"`+email+`","password":"correct horse battery"}`)
	if rec.Code != 201 {
		a.t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	a.cookies = rec.Result().Cookies()
}

func (a *api) bookID(query string) string {
	a.t.Helper()
	_, out := a.do("GET", "/api/books/search?q="+strings.ReplaceAll(query, " ", "+"), "")
	items := out["items"].([]any)
	return items[0].(map[string]any)["id"].(string)
}

func TestLibraryHTTPJourney(t *testing.T) {
	a := newAPI(t)
	if rec, _ := a.do("GET", "/api/library", ""); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	a.register("reader@athenaeum.dev")
	dune := a.bookID("dune")

	rec, e := a.do("POST", "/api/library", `{"book_id":"`+dune+`"}`)
	if rec.Code != 201 || e["status"] != "want_to_read" {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := a.do("POST", "/api/library", `{"book_id":"`+dune+`","status":"reading"}`); rec.Code != 200 {
		t.Fatalf("idempotent re-add: %d", rec.Code)
	}

	rec, e = a.do("PATCH", "/api/library/"+dune, `{"is_favorite":true,"progress_pct":40,"rating":5}`)
	if rec.Code != 200 || e["is_favorite"] != true || e["progress_pct"] != float64(40) || e["rating"] != float64(5) || e["status"] != "reading" {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	rec, e = a.do("PATCH", "/api/library/"+dune, `{"rating":null}`)
	if rec.Code != 200 || e["rating"] != nil || e["progress_pct"] != float64(40) {
		t.Fatalf("null clears: %s", rec.Body)
	}

	bad := map[string]string{
		`{"status":"finished"}`:  "VALIDATION_FAILED",
		`{"progress_pct":101}`:   "VALIDATION_FAILED",
		`{"rating":0}`:           "VALIDATION_FAILED",
		`{"is_favorite":"yes"}`:  "VALIDATION_FAILED",
		`{"progress_pct":"abc"}`: "VALIDATION_FAILED",
		`not json`:               "VALIDATION_FAILED",
	}
	for body, code := range bad {
		rec, out := a.do("PATCH", "/api/library/"+dune, body)
		if rec.Code != 400 || out["error"] != code {
			t.Errorf("%s → %d %s", body, rec.Code, rec.Body)
		}
	}
	if rec, out := a.do("PATCH", "/api/library/00000000-0000-0000-0000-000000000000", `{"status":"read"}`); rec.Code != 404 || out["error"] != "NOT_FOUND" {
		t.Fatalf("unknown book: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := a.do("POST", "/api/library", `{}`); rec.Code != 400 {
		t.Fatalf("missing book_id: %d", rec.Code)
	}

	_, out := a.do("GET", "/api/library?status=favorites", "")
	counts := out["counts"].(map[string]any)
	meta := out["meta"].(map[string]any)
	item := out["items"].([]any)[0].(map[string]any)
	if meta["total"] != float64(1) || counts["total"] != float64(1) || counts["favorites"] != float64(1) || counts["reading"] != float64(1) {
		t.Fatalf("list %v", out)
	}
	if item["kind"] != "entry" || item["book"].(map[string]any)["title"] != "Dune" ||
		item["location"].(map[string]any)["path"] != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("item %v", item)
	}
	if rec, out := a.do("GET", "/api/library?status=bogus", ""); rec.Code != 400 || out["error"] != "VALIDATION_FAILED" {
		t.Fatalf("bad status filter: %d", rec.Code)
	}

	// Book detail reflects my_library_entry.
	_, d := a.do("GET", "/api/books/"+dune, "")
	if me, _ := d["my_library_entry"].(map[string]any); me["status"] != "reading" || me["is_favorite"] != true {
		t.Fatalf("detail entry %v", d["my_library_entry"])
	}
	if me, _ := d["my_library_entry"].(map[string]any); me == nil || me["rating"] != nil {
		t.Fatalf("detail rating should be null: %v", d["my_library_entry"])
	} else if _, ok := me["rating"]; !ok {
		t.Fatalf("detail rating key missing: %v", me)
	}
	if rec, _ := a.do("PATCH", "/api/library/"+dune, `{"rating":3}`); rec.Code != 200 {
		t.Fatalf("rate: %d", rec.Code)
	}
	_, d = a.do("GET", "/api/books/"+dune, "")
	if me, _ := d["my_library_entry"].(map[string]any); me["rating"] != float64(3) {
		t.Fatalf("detail rating %v", d["my_library_entry"])
	}

	if rec, _ := a.do("DELETE", "/api/library/"+dune, ""); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	_, out = a.do("GET", "/api/library", "")
	if out["meta"].(map[string]any)["total"] != float64(0) {
		t.Fatalf("after delete: %v", out["meta"])
	}
}

func TestZeroCopyBookConflictThenWaitlist(t *testing.T) {
	a := newAPI(t)
	a.register("holder@athenaeum.dev")
	oxford := a.bookID("Oxford Guide to Library Research")
	dune := a.bookID("dune")

	// 409 with suggested_actions on a zero-available book.
	rec, out := a.do("POST", "/api/library", `{"book_id":"`+oxford+`","status":"reading"}`)
	if rec.Code != 409 || out["error"] != "BOOK_NOT_AVAILABLE" {
		t.Fatalf("409: %d %s", rec.Code, rec.Body)
	}
	actions := out["suggested_actions"].([]any)
	if len(actions) != 2 || actions[0] != "reserve" || actions[1] != "notify_when_available" {
		t.Fatalf("suggested_actions %v", actions)
	}
	if _, out := a.do("GET", "/api/library", ""); out["meta"].(map[string]any)["total"] != float64(0) {
		t.Fatal("409 must not create a row")
	}
	// ...but want_to_read is fine, and ?force=true bypasses the soft warning.
	if rec, _ := a.do("POST", "/api/library", `{"book_id":"`+oxford+`","status":"want_to_read"}`); rec.Code != 201 {
		t.Fatalf("want_to_read: %d", rec.Code)
	}
	if rec, _ := a.do("DELETE", "/api/library/"+oxford, ""); rec.Code != 204 {
		t.Fatal("cleanup")
	}
	if rec, _ := a.do("POST", "/api/library?force=true", `{"book_id":"`+oxford+`","status":"reading"}`); rec.Code != 201 {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}

	// Waitlist: join (201) → again (200, same row) → list → leave.
	rec, w := a.do("POST", "/api/books/"+oxford+"/waitlist", "")
	if rec.Code != 201 || w["status"] != "waiting" || w["position"] != float64(1) {
		t.Fatalf("join: %d %s", rec.Code, rec.Body)
	}
	rec, w2 := a.do("POST", "/api/books/"+oxford+"/waitlist", "")
	if rec.Code != 200 || w2["id"] != w["id"] {
		t.Fatalf("second join: %d %s", rec.Code, rec.Body)
	}
	_, list := a.do("GET", "/api/waitlist", "")
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["book"].(map[string]any)["title"] != "The Oxford Guide to Library Research" {
		t.Fatalf("waitlist %v", list)
	}
	_, withHolds := a.do("GET", "/api/library?include_waitlist=true", "")
	if hold := withHolds["items"].([]any)[0].(map[string]any); hold["kind"] != "hold" || hold["waitlist"].(map[string]any)["position"] != float64(1) {
		t.Fatalf("include_waitlist %v", withHolds["items"])
	}

	// A book with copies on the shelf needs no waitlist.
	if rec, out := a.do("POST", "/api/books/"+dune+"/waitlist", ""); rec.Code != 409 || out["error"] != "CONFLICT" {
		t.Fatalf("available book: %d %s", rec.Code, rec.Body)
	}
	if rec, out := a.do("POST", "/api/books/00000000-0000-0000-0000-000000000000/waitlist", ""); rec.Code != 404 || out["error"] != "NOT_FOUND" {
		t.Fatalf("unknown book: %d", rec.Code)
	}
	if rec, _ := a.do("DELETE", "/api/books/"+oxford+"/waitlist", ""); rec.Code != 204 {
		t.Fatalf("leave: %d", rec.Code)
	}
	if _, list := a.do("GET", "/api/waitlist", ""); len(list["items"].([]any)) != 0 {
		t.Fatal("waitlist should be empty after leaving")
	}
}

// TestPatronScanAndSelfReturnJourney covers docs/11 §2a/§2b over the seeded catalog.
func TestPatronScanAndSelfReturnJourney(t *testing.T) {
	a := newAPI(t)
	if rec, _ := a.do("POST", "/api/books/scan", `{"code":"9780441013593"}`); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}
	a.register("scanner@athenaeum.dev")

	// ISBN → record, home shelf and copies left.
	rec, out := a.do("POST", "/api/books/scan", `{"code":"9780441013593"}`)
	if rec.Code != 200 || out["match"] != "book" || out["book"].(map[string]any)["title"] != "Dune" {
		t.Fatalf("isbn scan: %d %s", rec.Code, rec.Body)
	}
	av := out["availability"].(map[string]any)
	if av["total_copies"] != float64(3) || av["available_copies"] != float64(2) || out["returnable"] != false ||
		out["wayfinding"].(map[string]any)["path"] != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("isbn scan body %v", out)
	}
	if rec, out := a.do("POST", "/api/books/scan", `{"code":"9780441013590"}`); rec.Code != 400 || out["error"] != "INVALID_ISBN" {
		t.Fatalf("bad checksum: %d %s", rec.Code, rec.Body)
	}
	if rec, out := a.do("POST", "/api/books/scan", `{"code":"9780306406157"}`); rec.Code != 200 || out["in_catalog"] != false || out["match"] != "none" {
		t.Fatalf("not in catalog: %d %s", rec.Code, rec.Body)
	}

	// Copy tag → returnable copy → return.
	rec, out = a.do("POST", "/api/books/scan", `{"code":"ath-002941-c01"}`)
	cp, _ := out["copy"].(map[string]any)
	if rec.Code != 200 || out["match"] != "copy" || out["returnable"] != true || cp["status"] != "checked_out" {
		t.Fatalf("copy scan: %d %s", rec.Code, rec.Body)
	}
	copyID := cp["id"].(string)
	rec, out = a.do("POST", "/api/copies/"+copyID+"/return", "")
	if rec.Code != 200 || out["copy"].(map[string]any)["status"] != "available" || out["hold"] != nil ||
		out["availability"].(map[string]any)["available_copies"] != float64(3) {
		t.Fatalf("return: %d %s", rec.Code, rec.Body)
	}
	rec, out = a.do("POST", "/api/copies/"+copyID+"/return", "")
	if rec.Code != 409 || out["error"] != "CONFLICT" || out["current_status"] != "available" {
		t.Fatalf("second return: %d %s", rec.Code, rec.Body)
	}
	if rec, out := a.do("POST", "/api/copies/00000000-0000-0000-0000-000000000000/return", ""); rec.Code != 404 || out["error"] != "NOT_FOUND" {
		t.Fatalf("unknown copy: %d %s", rec.Code, rec.Body)
	}
	if _, out := a.do("POST", "/api/books/scan", `{"code":"ATH-002941-C01"}`); out["returnable"] != false {
		t.Fatalf("rescan after return: %v", out)
	}
}

type unitEmb struct{}

func (unitEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, 768)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func TestAdminBulkDeleteHTTP(t *testing.T) {
	a := newAPI(t)
	path := "/api/admin/books/bulk-delete"
	if rec, _ := a.do("POST", path, `{}`); rec.Code != 401 {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	a.register("bulk-admin@athenaeum.dev")
	if rec, _ := a.do("POST", path, `{}`); rec.Code != 403 {
		t.Fatalf("patron: %d", rec.Code)
	}
	if _, err := a.pool.Exec(context.Background(), `UPDATE users SET role='admin' WHERE email='bulk-admin@athenaeum.dev'`); err != nil {
		t.Fatal(err)
	}
	rec, _ := a.do("POST", "/api/auth/login", `{"email":"bulk-admin@athenaeum.dev","password":"correct horse battery"}`)
	a.cookies = rec.Result().Cookies()
	for _, body := range []string{`{}`, `{"ids":[]}`, `{"ids":[null]}`, `{"ids":["bad"]}`, `not json`} {
		if rec, out := a.do("POST", path, body); rec.Code != 400 || out["error"] != "VALIDATION_FAILED" {
			t.Fatalf("invalid %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	var blocked, removable string
	if err := a.pool.QueryRow(context.Background(), `SELECT b.id FROM books b JOIN book_copies c ON c.book_id=b.id WHERE c.status='checked_out' LIMIT 1`).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(context.Background(), `SELECT b.id FROM books b WHERE NOT EXISTS (SELECT 1 FROM book_copies c WHERE c.book_id=b.id AND c.status IN ('checked_out','reserved_waitlist')) LIMIT 1`).Scan(&removable); err != nil {
		t.Fatal(err)
	}
	body := `{"ids":["` + removable + `","` + blocked + `"]}`
	rec, out := a.do("POST", path, body)
	items, ok := out["blocked"].([]any)
	if rec.Code != 409 || out["error"] != "CONFLICT" || !ok || len(items) != 1 {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
	item := items[0].(map[string]any)
	if item["id"] != blocked || item["title"] == "" || item["active_loans"] == nil {
		t.Fatalf("blocked item: %v", item)
	}
	if rec, _ := a.do("POST", path, `{"ids":["`+removable+`"]}`); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := a.do("POST", path, `{"ids":["`+removable+`"]}`); rec.Code != 404 {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}
}

func TestIndexStatusDrainsAfterWorkerRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newAPI(t)
	a.register("archivist@athenaeum.dev")
	if rec, _ := a.do("GET", "/api/admin/index-status", ""); rec.Code != 403 {
		t.Fatalf("patron: %d", rec.Code)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE email='archivist@athenaeum.dev'`); err != nil {
		t.Fatal(err)
	}
	rec, _ := a.do("POST", "/api/auth/login", `{"email":"archivist@athenaeum.dev","password":"correct horse battery"}`)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	a.cookies = rec.Result().Cookies()

	// Fresh seed has no embeddings: every described book is pending.
	rec, st := a.do("GET", "/api/admin/index-status", "")
	ids, _ := st["pending_book_ids"].([]any)
	if rec.Code != 200 || st["pending_count"] != float64(40) || len(ids) != 40 {
		t.Fatalf("fresh index-status %d %s", rec.Code, rec.Body)
	}
	for _, mode := range []string{"", "&mode=keyword"} {
		_, out := a.do("GET", "/api/books/search?per_page=1"+mode, "")
		if m := out["meta"].(map[string]any); m["index_pending"] != float64(40) {
			t.Fatalf("search meta%s %v", mode, m)
		}
	}

	store := repository.NewIndexStore(a.pool)
	w := index.NewWorker(store, index.New(store, unitEmb{}, "nomic-embed-text", logging.Discard()), logging.Discard()).
		WithTimings(time.Hour, time.Millisecond, 10*time.Millisecond)
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, st = a.do("GET", "/api/admin/index-status", "")
		if st["pending_count"] == float64(0) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backlog never drained: %v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	rec, _ = a.do("GET", "/api/admin/index-status", "")
	if rec.Body.String() != `{"pending_count":0,"pending_book_ids":[]}` {
		t.Fatalf("drained body %s", rec.Body)
	}
	_, out := a.do("GET", "/api/books/search?per_page=1", "")
	if m := out["meta"].(map[string]any); m["index_pending"] != float64(0) {
		t.Fatalf("drained search meta %v", m)
	}
}
