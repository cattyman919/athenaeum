package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

type fakeRepo struct {
	kw, vec   []model.SearchHit
	books     map[uuid.UUID]model.BookSummary
	gotFilter model.SearchFilters
	vecLimit  int
	detail    model.BookDetailData
	detailErr error
}

func (f *fakeRepo) Keyword(_ context.Context, flt model.SearchFilters, _ string) ([]model.SearchHit, error) {
	f.gotFilter = flt
	return f.kw, nil
}

func (f *fakeRepo) Semantic(_ context.Context, flt model.SearchFilters, _ []float32, limit int) ([]model.SearchHit, error) {
	f.gotFilter, f.vecLimit = flt, limit
	return f.vec, nil
}

func (f *fakeRepo) Summaries(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]model.BookSummary, error) {
	out := map[uuid.UUID]model.BookSummary{}
	for _, id := range ids {
		if b, ok := f.books[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

func (f *fakeRepo) Detail(context.Context, uuid.UUID, uuid.UUID) (model.BookDetailData, error) {
	return f.detail, f.detailErr
}

type fakeEmb struct{ err error }

func (f fakeEmb) EmbedQuery(context.Context, string) ([]float32, error) {
	return []float32{1}, f.err
}

func yr(n int) *int { return &n }

func book(title string, year *int, total, avail int, statuses ...string) model.BookSummary {
	return model.BookSummary{
		ID: uuid.New(), Title: title, PublishedYear: year, Statuses: statuses,
		Availability: model.Availability{TotalCopies: total, AvailableCopies: avail},
	}
}

func setup(books ...model.BookSummary) (*fakeRepo, []uuid.UUID) {
	r := &fakeRepo{books: map[uuid.UUID]model.BookSummary{}}
	var ids []uuid.UUID
	for _, b := range books {
		r.books[b.ID] = b
		ids = append(ids, b.ID)
	}
	return r, ids
}

func hits(ids ...uuid.UUID) []model.SearchHit {
	out := make([]model.SearchHit, len(ids))
	for i, id := range ids {
		out[i] = model.SearchHit{BookID: id, Score: 1 - float64(i)/10}
	}
	return out
}

func titles(p model.SearchPage) string {
	var ts []string
	for _, b := range p.Items {
		ts = append(ts, b.Title)
	}
	return strings.Join(ts, ",")
}

func TestAvailabilityState(t *testing.T) {
	tests := []struct {
		total, avail int
		statuses     []string
		want         string
	}{
		{3, 2, []string{"available", "checked_out"}, "available"},
		{0, 0, nil, "not_in_building"},
		{2, 0, []string{"checked_out"}, "checked_out"},
		{1, 0, []string{"missing_lost"}, "missing_lost"},
		{2, 0, []string{"checked_out", "missing_lost"}, "checked_out"}, // degradation rule
		{2, 0, []string{"reserved_waitlist"}, "reserved_waitlist"},
	}
	for _, tt := range tests {
		if got := catalog.AvailabilityState(tt.total, tt.avail, tt.statuses); got != tt.want {
			t.Errorf("%+v → %s, want %s", tt, got, tt.want)
		}
	}
}

func TestNormalizeValidation(t *testing.T) {
	bad := []catalog.Request{
		{Mode: "fuzzy"},
		{Mode: "semantic"},
		{Mode: "hybrid", Query: "  "},
		{Filters: model.SearchFilters{Sections: []string{"XX"}}},
		{Filters: model.SearchFilters{Eras: []string{"medieval"}}},
		{Sort: "popularity"},
		{PerPage: 51},
		{Page: -1},
	}
	for i, r := range bad {
		_, err := catalog.Normalize(r)
		if de, ok := model.AsError(err); !ok || de.Code != model.CodeValidationFailed {
			t.Errorf("case %d: %v", i, err)
		}
	}
	r, err := catalog.Normalize(catalog.Request{})
	if err != nil || r.Mode != "keyword" || r.Page != 1 || r.PerPage != 20 || r.Sort != "relevance" {
		t.Fatalf("defaults %+v %v", r, err)
	}
}

func TestFuseRRFMatchesFormula(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	kw := hits(a, b)  // a rank1, b rank2
	vec := hits(b, c) // b rank1, c rank2
	got := catalog.FuseRRF(kw, vec)
	if len(got) != 3 || got[0].BookID != b {
		t.Fatalf("%+v", got)
	}
	wantB := 1.0/62 + 1.0/61
	if d := got[0].Score - wantB; d > 1e-12 || d < -1e-12 {
		t.Fatalf("score %v want %v", got[0].Score, wantB)
	}
	if got[1].BookID != a || got[2].BookID != c { // a: 1/61 > c: 1/62
		t.Fatalf("order %+v", got)
	}
}

func TestSearchKeywordPaginationAndMeta(t *testing.T) {
	var bs []model.BookSummary
	for _, tt := range []string{"A", "B", "C", "D", "E"} {
		bs = append(bs, book(tt, yr(2000), 1, 1, "available"))
	}
	repo, ids := setup(bs...)
	repo.kw = hits(ids...)
	svc := catalog.New(repo, fakeEmb{})

	p, err := svc.Search(context.Background(), catalog.Request{Query: "x", PerPage: 2, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if titles(p) != "C,D" || p.Meta != (model.PageMeta{Page: 2, PerPage: 2, Total: 5, TotalPages: 3}) {
		t.Fatalf("%s %+v", titles(p), p.Meta)
	}
	if p.Items[0].Availability.State != "available" {
		t.Fatalf("state not resolved: %+v", p.Items[0].Availability)
	}
	p, _ = svc.Search(context.Background(), catalog.Request{Query: "x", PerPage: 2, Page: 9})
	if len(p.Items) != 0 || p.Meta.Total != 5 {
		t.Fatalf("out-of-range page: %+v", p)
	}
}

func TestSearchSortModes(t *testing.T) {
	repo, ids := setup(
		book("Zeta", yr(1990), 1, 1, "available"),
		book("alpha", yr(1965), 1, 0, "checked_out"),
		book("Mid", nil, 0, 0),
		book("Beta", yr(1990), 1, 1, "available"),
	)
	repo.kw = hits(ids...)
	svc := catalog.New(repo, fakeEmb{})
	run := func(sort, q string) string {
		p, err := svc.Search(context.Background(), catalog.Request{Query: q, Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
		return titles(p)
	}
	if got := run("relevance", "x"); got != "Zeta,alpha,Mid,Beta" {
		t.Errorf("relevance %s", got)
	}
	if got := run("title", "x"); got != "alpha,Beta,Mid,Zeta" {
		t.Errorf("title %s", got)
	}
	if got := run("year:asc", "x"); got != "alpha,Beta,Zeta,Mid" {
		t.Errorf("year:asc %s", got)
	}
	if got := run("year:desc", "x"); got != "Beta,Zeta,alpha,Mid" {
		t.Errorf("year:desc %s", got)
	}
	// Relevance without a query degrades to title order.
	if got := run("relevance", ""); got != "alpha,Beta,Mid,Zeta" {
		t.Errorf("empty q relevance %s", got)
	}
}

func TestSearchSemanticAndHybrid(t *testing.T) {
	a, b, c := book("A", yr(1), 1, 1, "available"), book("B", yr(1), 1, 1, "available"), book("C", yr(1), 1, 1, "available")
	repo, _ := setup(a, b, c)
	repo.kw = hits(a.ID, b.ID)
	repo.vec = hits(b.ID, c.ID)
	svc := catalog.New(repo, fakeEmb{})

	p, err := svc.Search(context.Background(), catalog.Request{Query: "q", Mode: "semantic"})
	if err != nil || titles(p) != "B,C" || repo.vecLimit != catalog.VectorTopK {
		t.Fatalf("semantic %s %v limit=%d", titles(p), err, repo.vecLimit)
	}
	p, err = svc.Search(context.Background(), catalog.Request{Query: "q", Mode: "hybrid"})
	if err != nil || titles(p) != "B,A,C" {
		t.Fatalf("hybrid %s %v", titles(p), err)
	}
}

func TestSearchPassesFiltersAndMapsEmbedderOutage(t *testing.T) {
	repo, _ := setup()
	svc := catalog.New(repo, fakeEmb{})
	f := model.SearchFilters{Sections: []string{"SF"}, AvailableOnly: true, Eras: []string{"2000_n"}}
	if _, err := svc.Search(context.Background(), catalog.Request{Filters: f}); err != nil {
		t.Fatal(err)
	}
	if repo.gotFilter.Sections[0] != "SF" || !repo.gotFilter.AvailableOnly || repo.gotFilter.Eras[0] != "2000_n" {
		t.Fatalf("filters %+v", repo.gotFilter)
	}

	down := model.New(model.CodeEmbeddingUnavailable, "down")
	svc = catalog.New(repo, fakeEmb{err: down})
	for _, mode := range []string{"semantic", "hybrid"} {
		_, err := svc.Search(context.Background(), catalog.Request{Query: "q", Mode: mode})
		if !errors.Is(err, model.ErrEmbeddingUnavail) {
			t.Errorf("%s: %v", mode, err)
		}
	}
	// Keyword search keeps working while Ollama is down.
	if _, err := svc.Search(context.Background(), catalog.Request{Query: "q"}); err != nil {
		t.Fatalf("keyword must not need embeddings: %v", err)
	}
}

func TestLadderAndSteps(t *testing.T) {
	l := model.LocationRow{
		BuildingName: "Main Building", FloorNumber: 3, FloorName: "North Stacks", SectionName: "Science Fiction & Speculative",
		AisleCode: "A-1", ShelfCode: "S-2", RowLevel: 4, HeightTier: "Eye Level",
	}
	want := "Main Building (North Stacks) → Floor 3 → Science Fiction & Speculative → Aisle A-1 → Shelf S-2 (Row 4, Eye Level)"
	if got := catalog.Ladder(l); got != want {
		t.Fatalf("%s", got)
	}
	steps := catalog.LadderSteps(l)
	if len(steps) != 4 || steps[0] != "Floor 3 — North Stacks" || steps[3] != "Shelf S-2 — Row 4, Eye Level" {
		t.Fatalf("%v", steps)
	}
}

func TestDetailStateAndHomeShelf(t *testing.T) {
	c1, c2, c3 := uuid.New(), uuid.New(), uuid.New()
	loc := func(id uuid.UUID, path string) model.LocationRow {
		return model.LocationRow{CopyID: id, Path: path, BuildingName: "B", FloorName: "F", SectionName: "S", AisleCode: "A", ShelfCode: "S1", RowLevel: 1, HeightTier: "T"}
	}
	repo := &fakeRepo{detail: model.BookDetailData{
		Detail: model.BookDetail{
			Title:        "Dune",
			Availability: model.DetailAvailability{TotalCopies: 3, AvailableCopies: 2},
			Copies: []model.CopyDetail{
				{ID: c1, Barcode: "C01", Status: "checked_out"},
				{ID: c2, Barcode: "C02", Status: "available"},
				{ID: c3, Barcode: "C03", Status: "available"},
			},
		},
		Locations: map[uuid.UUID]model.LocationRow{c1: loc(c1, "P/1"), c2: loc(c2, "P/2"), c3: loc(c3, "P/3")},
		Statuses:  []string{"available", "checked_out"},
	}}
	svc := catalog.New(repo, fakeEmb{})
	d, err := svc.Detail(context.Background(), uuid.NewString(), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Availability.State != "available" || d.Wayfinding == nil || d.Wayfinding.Path != "P/2" {
		t.Fatalf("home shelf must be the first available copy: %+v %+v", d.Availability, d.Wayfinding)
	}
	if d.Copies[0].Location == nil || d.Copies[0].Location.Path != "P/1" || !strings.Contains(d.Copies[0].Location.Ladder, "Floor 0") {
		t.Fatalf("copy location %+v", d.Copies[0].Location)
	}

	// All copies out → shared status, home shelf falls back to any shelved copy.
	repo.detail.Detail.Copies = repo.detail.Detail.Copies[:1]
	repo.detail.Detail.Availability = model.DetailAvailability{TotalCopies: 1}
	repo.detail.Statuses = []string{"checked_out"}
	d, _ = svc.Detail(context.Background(), uuid.NewString(), uuid.Nil)
	if d.Availability.State != "checked_out" || d.Wayfinding == nil || d.Wayfinding.Path != "P/1" {
		t.Fatalf("%+v %+v", d.Availability, d.Wayfinding)
	}

	// No copies → not_in_building, no wayfinding.
	repo.detail = model.BookDetailData{Detail: model.BookDetail{Title: "Z", Copies: []model.CopyDetail{}}}
	d, _ = svc.Detail(context.Background(), uuid.NewString(), uuid.Nil)
	if d.Availability.State != "not_in_building" || d.Wayfinding != nil {
		t.Fatalf("%+v %+v", d.Availability, d.Wayfinding)
	}
}

func TestDetailBadIDAndMissingBook(t *testing.T) {
	repo := &fakeRepo{detailErr: model.New(model.CodeNotFound, "Book not found.")}
	svc := catalog.New(repo, fakeEmb{})
	for _, id := range []string{"not-a-uuid", uuid.NewString()} {
		if _, err := svc.Detail(context.Background(), id, uuid.Nil); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s: %v", id, err)
		}
	}
}
