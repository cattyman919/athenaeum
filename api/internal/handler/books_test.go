package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

type stubCatalogRepo struct {
	id   uuid.UUID
	gotQ string
	gotF model.SearchFilters
}

func (s *stubCatalogRepo) Keyword(_ context.Context, f model.SearchFilters, q string) ([]model.SearchHit, error) {
	s.gotQ, s.gotF = q, f
	return []model.SearchHit{{BookID: s.id, Score: 0.9}}, nil
}

func (s *stubCatalogRepo) Semantic(context.Context, model.SearchFilters, []float32, int) ([]model.SearchHit, error) {
	return nil, nil
}

func (s *stubCatalogRepo) Summaries(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]model.BookSummary, error) {
	year := 1965
	return map[uuid.UUID]model.BookSummary{ids[0]: {
		ID: ids[0], Title: "Dune", Authors: []string{"Frank Herbert"}, PublishedYear: &year,
		Availability: model.Availability{TotalCopies: 3, AvailableCopies: 2},
		Statuses:     []string{"available", "checked_out"},
	}}, nil
}

type downEmbedder struct{}

func (downEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, model.New(model.CodeEmbeddingUnavailable, "Embedding service unavailable.")
}

func searchApp(t *testing.T) (http.Handler, *stubCatalogRepo, *http.Cookie) {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	repo := &stubCatalogRepo{id: uuid.New()}
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Catalog: catalog.New(repo, downEmbedder{}),
	})
	tok, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	return r, repo, &http.Cookie{Name: middleware.AccessCookie, Value: tok}
}

func get(r http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSearchEndpoint(t *testing.T) {
	r, repo, cookie := searchApp(t)

	if rec := get(r, "/api/books/search?q=dune", nil); rec.Code != 401 {
		t.Fatalf("anon: %d", rec.Code)
	}

	rec := get(r, "/api/books/search?q=dune&section=SF&section=REF&availability=available&era=2000_n&per_page=10", cookie)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Meta  map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	it := body.Items[0]
	av := it["availability"].(map[string]any)
	if it["title"] != "Dune" || av["state"] != "available" || av["total_copies"] != float64(3) || it["score"] != 0.9 {
		t.Fatalf("item %v", it)
	}
	if _, leaked := it["Statuses"]; leaked {
		t.Fatal("internal field leaked")
	}
	if body.Meta["total"] != float64(1) || body.Meta["per_page"] != float64(10) || body.Meta["total_pages"] != float64(1) {
		t.Fatalf("meta %v", body.Meta)
	}
	if repo.gotQ != "dune" || len(repo.gotF.Sections) != 2 || !repo.gotF.AvailableOnly || repo.gotF.Eras[0] != "2000_n" {
		t.Fatalf("filters not forwarded: %q %+v", repo.gotQ, repo.gotF)
	}
}

func TestSearchEndpointErrors(t *testing.T) {
	r, _, cookie := searchApp(t)
	cases := map[string]struct {
		path   string
		status int
		code   string
	}{
		"bad mode":          {"/api/books/search?mode=fuzzy", 400, "VALIDATION_FAILED"},
		"semantic needs q":  {"/api/books/search?mode=semantic", 400, "VALIDATION_FAILED"},
		"bad per_page":      {"/api/books/search?per_page=999", 400, "VALIDATION_FAILED"},
		"non-numeric page":  {"/api/books/search?page=abc", 400, "VALIDATION_FAILED"},
		"bad availability":  {"/api/books/search?availability=maybe", 400, "VALIDATION_FAILED"},
		"embedder down":     {"/api/books/search?mode=semantic&q=space", 503, "EMBEDDING_UNAVAILABLE"},
		"hybrid embed down": {"/api/books/search?mode=hybrid&q=space", 503, "EMBEDDING_UNAVAILABLE"},
	}
	for name, tc := range cases {
		rec := get(r, tc.path, cookie)
		if rec.Code != tc.status || errCode(t, rec) != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}
