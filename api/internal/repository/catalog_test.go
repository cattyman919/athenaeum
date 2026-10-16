package repository_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
	"github.com/cattyman919/athenaeum/api/internal/testutil/pgtest"
)

// titleEmb gives every book title its own one-hot axis, so cosine similarity between a
// query "about title X" and book X is exactly 1 and 0 for every other book.
type titleEmb struct{ axis map[string]int }

func newTitleEmb() *titleEmb { return &titleEmb{axis: map[string]int{}} }

func (e *titleEmb) vec(title string) []float32 {
	i, ok := e.axis[title]
	if !ok {
		i = len(e.axis)
		e.axis[title] = i
	}
	v := make([]float32, 768)
	v[i] = 1
	return v
}

func (e *titleEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		title, _, _ := strings.Cut(t, " by ")
		out[i] = e.vec(title)
	}
	return out, nil
}

func (e *titleEmb) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	return e.vec(text), nil
}

func seededCatalog(t *testing.T) (*pgxpool.Pool, *catalog.Service) {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.New(t)
	fx, err := seed.Load("../../mocks")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Run(ctx, repository.NewSeedStore(pool), nil, fx, seed.Options{}, logging.Discard()); err != nil {
		t.Fatal(err)
	}
	emb := newTitleEmb()
	if _, err := index.New(repository.NewIndexStore(pool), emb, "test-model", logging.Discard()).Run(ctx, ""); err != nil {
		t.Fatal(err)
	}
	return pool, catalog.New(repository.NewCatalogRepository(pool), emb)
}

func search(t *testing.T, svc *catalog.Service, r catalog.Request) model.SearchPage {
	t.Helper()
	if r.PerPage == 0 {
		r.PerPage = 50
	}
	p, err := svc.Search(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func firstTitles(p model.SearchPage, n int) []string {
	var out []string
	for i, b := range p.Items {
		if i == n {
			break
		}
		out = append(out, b.Title)
	}
	return out
}

func TestCatalogBrowseAndFacets(t *testing.T) {
	_, svc := seededCatalog(t)

	all := search(t, svc, catalog.Request{})
	if all.Meta.Total != 40 {
		t.Fatalf("browse total %d", all.Meta.Total)
	}
	if len(all.Items) != 40 {
		t.Fatal("browse should return everything")
	}

	// docs/13 §5: availability=available → 35 of 40 books.
	if p := search(t, svc, catalog.Request{Filters: model.SearchFilters{AvailableOnly: true}}); p.Meta.Total != 35 {
		t.Fatalf("available-only %d, want 35", p.Meta.Total)
	}
	// SF section = 10 books.
	sf := search(t, svc, catalog.Request{Filters: model.SearchFilters{Sections: []string{"SF"}}})
	if sf.Meta.Total != 10 {
		t.Fatalf("SF %d", sf.Meta.Total)
	}
	for _, b := range sf.Items {
		if b.Section == nil || b.Section.Code != "SF" || b.Section.Color != "#3B82F6" {
			t.Fatalf("section payload %+v", b.Section)
		}
	}
	// Two sections combine with OR.
	if p := search(t, svc, catalog.Request{Filters: model.SearchFilters{Sections: []string{"SF", "REF"}}}); p.Meta.Total != 16 {
		t.Fatalf("SF+REF %d", p.Meta.Total)
	}
	// Era buckets partition the catalog by published_year.
	total := 0
	for _, era := range []string{"pre1800", "1800_1970", "1970_2000", "2000_n"} {
		total += search(t, svc, catalog.Request{Filters: model.SearchFilters{Eras: []string{era}}}).Meta.Total
	}
	if total != 40 {
		t.Fatalf("era buckets cover %d books, want 40", total)
	}
	if p := search(t, svc, catalog.Request{Filters: model.SearchFilters{Eras: []string{"pre1800"}}}); p.Meta.Total != 0 {
		t.Fatalf("pre1800 %d", p.Meta.Total)
	}
	// Combined filters intersect: SF ∧ available ∧ 2000_n.
	p := search(t, svc, catalog.Request{Filters: model.SearchFilters{Sections: []string{"SF"}, AvailableOnly: true, Eras: []string{"2000_n"}}})
	for _, b := range p.Items {
		if b.Section.Code != "SF" || b.Availability.State != "available" || *b.PublishedYear < 2000 {
			t.Fatalf("filter leak: %+v", b)
		}
	}
	if p.Meta.Total != 2 { // The Three-Body Problem (2008), Project Hail Mary (2021)
		t.Fatalf("SF∧available∧2000+ = %d", p.Meta.Total)
	}
}

func TestCatalogItemPayload(t *testing.T) {
	_, svc := seededCatalog(t)
	p := search(t, svc, catalog.Request{Query: "dune"})
	if len(p.Items) == 0 || p.Items[0].Title != "Dune" {
		t.Fatalf("keyword 'dune' → %v", firstTitles(p, 3))
	}
	d := p.Items[0]
	if d.Availability.TotalCopies != 3 || d.Availability.AvailableCopies != 2 || d.Availability.State != "available" {
		t.Fatalf("availability %+v", d.Availability)
	}
	if d.Location == nil || d.Location.Path != "MAIN/3F/SF/A-1/S-2" || d.Location.ShelfCode != "S-2" {
		t.Fatalf("location %+v", d.Location)
	}
	if d.ISBN13 == nil || *d.ISBN13 != "9780441013593" || d.CoverURL == nil || !strings.Contains(*d.CoverURL, "9780441013593") {
		t.Fatalf("isbn/cover %v %v", d.ISBN13, d.CoverURL)
	}
	if d.Score <= 0 {
		t.Fatalf("score %v", d.Score)
	}

	// Zero-copy book: not_in_building, no location (docs/13 §5 edge case).
	z := search(t, svc, catalog.Request{Query: "Oxford Guide to Library Research"})
	if len(z.Items) == 0 || z.Items[0].Availability.State != "not_in_building" || z.Items[0].Location != nil {
		t.Fatalf("zero-copy %+v", z.Items)
	}
	// Fully checked-out book would be checked_out; Hyperion has C01 out, C02 available.
	h := search(t, svc, catalog.Request{Query: "Hyperion"})
	if h.Items[0].Availability.AvailableCopies != 1 {
		t.Fatalf("hyperion %+v", h.Items[0].Availability)
	}
}

func TestKeywordSearchBehaviour(t *testing.T) {
	_, svc := seededCatalog(t)

	// Author search.
	p := search(t, svc, catalog.Request{Query: "Herbert"})
	if len(p.Items) == 0 || p.Items[0].Title != "Dune" {
		t.Fatalf("author query: %v", firstTitles(p, 3))
	}
	// Description (FTS, stemmed): "desert" is in Dune's blurb.
	p = search(t, svc, catalog.Request{Query: "desert planet"})
	if len(p.Items) == 0 || p.Items[0].Title != "Dune" {
		t.Fatalf("fts query: %v", firstTitles(p, 3))
	}
	// Typo tolerance via trigram similarity.
	p = search(t, svc, catalog.Request{Query: "Neuromancr"})
	if len(p.Items) == 0 || p.Items[0].Title != "Neuromancer" {
		t.Fatalf("typo query: %v", firstTitles(p, 3))
	}
	// No match → empty, not an error.
	if p := search(t, svc, catalog.Request{Query: "zzzzqqqq"}); p.Meta.Total != 0 {
		t.Fatalf("no-match total %d", p.Meta.Total)
	}
	// Wildcard characters are plain text.
	if p := search(t, svc, catalog.Request{Query: "100%"}); p.Meta.Total != 0 {
		t.Fatalf("percent literal matched %d", p.Meta.Total)
	}
	// Filters apply to keyword hits.
	p = search(t, svc, catalog.Request{Query: "the", Filters: model.SearchFilters{Sections: []string{"REF"}}})
	for _, b := range p.Items {
		if b.Section.Code != "REF" {
			t.Fatalf("filter leak %+v", b.Section)
		}
	}
	// Sorting by year.
	p = search(t, svc, catalog.Request{Sort: "year:desc", Filters: model.SearchFilters{Sections: []string{"SF"}}})
	if *p.Items[0].PublishedYear != 2021 || *p.Items[len(p.Items)-1].PublishedYear != 1932 {
		t.Fatalf("year:desc %d..%d", *p.Items[0].PublishedYear, *p.Items[len(p.Items)-1].PublishedYear)
	}
}

func TestSemanticAndHybridSearch(t *testing.T) {
	_, svc := seededCatalog(t)

	p := search(t, svc, catalog.Request{Query: "Snow Crash", Mode: "semantic"})
	if p.Meta.Total == 0 || p.Items[0].Title != "Snow Crash" || p.Items[0].Score < 0.99 {
		t.Fatalf("semantic: %v score=%v", firstTitles(p, 3), p.Items[0].Score)
	}
	if p.Meta.Total > catalog.VectorTopK {
		t.Fatalf("semantic returns at most %d books, got %d", catalog.VectorTopK, p.Meta.Total)
	}

	// Filters constrain the vector half too.
	p = search(t, svc, catalog.Request{Query: "Snow Crash", Mode: "semantic", Filters: model.SearchFilters{Sections: []string{"HVA"}}})
	for _, b := range p.Items {
		if b.Section.Code != "HVA" {
			t.Fatalf("semantic filter leak %+v", b.Section)
		}
	}

	// Hybrid: a keyword-only hit (author query matches nothing in the vector space)
	// still appears, and a book in both lists outranks single-list books.
	p = search(t, svc, catalog.Request{Query: "Dune", Mode: "hybrid"})
	if p.Items[0].Title != "Dune" {
		t.Fatalf("hybrid: %v", firstTitles(p, 3))
	}
	want := 1.0/61 + 1.0/61 // rank 1 in both lists
	if d := p.Items[0].Score - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("hybrid score %v, want %v", p.Items[0].Score, want)
	}
}

func TestBookDetail(t *testing.T) {
	ctx := context.Background()
	pool, svc := seededCatalog(t)

	var duneID, zeroID string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM books WHERE title = 'Dune'`).Scan(&duneID)
	_ = pool.QueryRow(ctx, `SELECT id::text FROM books WHERE title = 'The Oxford Guide to Library Research'`).Scan(&zeroID)

	d, err := svc.Detail(ctx, duneID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Dune" || d.Publisher == nil || *d.Publisher != "Ace Books" || d.DeweyNum == nil || *d.DeweyNum != "813.54" ||
		d.LOCNum == nil || *d.LOCNum != "PS3558.E63 D8 1965" || d.ISBN13 == nil {
		t.Fatalf("metadata %+v", d)
	}
	if d.Section == nil || d.Section.Code != "SF" {
		t.Fatalf("section %+v", d.Section)
	}
	if d.Availability.TotalCopies != 3 || d.Availability.AvailableCopies != 2 || d.Availability.State != "available" {
		t.Fatalf("availability %+v", d.Availability)
	}
	if len(d.Copies) != 3 || d.Copies[0].Barcode != "ATH-002941-C01" || d.Copies[0].Status != "checked_out" ||
		d.Copies[2].Barcode != "ATH-002941-C03" || d.Copies[2].Location.Path != "MAIN/3F/SF/A-1/S-3" {
		t.Fatalf("copies %+v", d.Copies)
	}
	wantLadder := "Athenaeum Main Building (North Stacks) → Floor 3 → Science Fiction & Speculative → Aisle A-1 → Shelf S-2 (Row 4, Eye Level)"
	if d.Copies[1].Location == nil || d.Copies[1].Location.Ladder != wantLadder {
		t.Fatalf("ladder %+v", d.Copies[1].Location)
	}
	if d.Wayfinding == nil || d.Wayfinding.Path != "MAIN/3F/SF/A-1/S-2" || len(d.Wayfinding.LadderSteps) != 4 ||
		d.Wayfinding.LadderSteps[0] != "Floor 3 — North Stacks" || d.Wayfinding.LadderSteps[3] != "Shelf S-2 — Row 4, Eye Level" {
		t.Fatalf("wayfinding %+v", d.Wayfinding)
	}
	wantParts := model.LocationParts{
		BuildingCode: "MAIN", BuildingName: "Athenaeum Main Building", FloorNumber: 3, FloorName: "North Stacks",
		SectionCode: "SF", SectionName: "Science Fiction & Speculative", AisleCode: "A-1", ShelfCode: "S-2",
		RowLevel: 4, HeightTier: "Eye Level",
	}
	if d.Wayfinding.Parts != wantParts || d.Copies[1].Location.Parts != wantParts {
		t.Fatalf("parts %+v / %+v", d.Wayfinding.Parts, d.Copies[1].Location.Parts)
	}
	if p := d.Copies[2].Location.Parts; p.ShelfCode != "S-3" || p.SectionCode != "SF" || p.BuildingCode != "MAIN" {
		t.Fatalf("copy 3 parts %+v", p)
	}
	if len(d.Neighbors) == 0 || len(d.Neighbors) > 8 {
		t.Fatalf("neighbors %d", len(d.Neighbors))
	}
	titles := map[string]bool{}
	for _, n := range d.Neighbors {
		titles[n.Title] = true
	}
	if titles["Dune"] || !titles["Neuromancer"] { // same shelf A-1/S-2 first
		t.Fatalf("neighbors %v", titles)
	}
	if d.MyLibraryEntry != nil {
		t.Fatalf("no library entry expected: %+v", d.MyLibraryEntry)
	}

	// my_library_entry reflects the caller's row.
	uid := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, 'x@athenaeum.dev', 'X')`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_library (id, user_id, book_id, status, is_favorite) VALUES ($1,$2,$3::uuid,'reading',true)`,
		uuid.New(), uid, duneID); err != nil {
		t.Fatal(err)
	}
	d, _ = svc.Detail(ctx, duneID, uid)
	if d.MyLibraryEntry == nil || d.MyLibraryEntry.Status != "reading" || !d.MyLibraryEntry.IsFavorite || d.MyLibraryEntry.Rating != nil {
		t.Fatalf("library entry %+v", d.MyLibraryEntry)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_library SET rating = 4 WHERE user_id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	d, _ = svc.Detail(ctx, duneID, uid)
	if d.MyLibraryEntry == nil || d.MyLibraryEntry.Rating == nil || *d.MyLibraryEntry.Rating != 4 {
		t.Fatalf("library entry rating %+v", d.MyLibraryEntry)
	}

	// Zero-copy book: 200 payload, not_in_building, empty manifest, no wayfinding.
	z, err := svc.Detail(ctx, zeroID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if z.Availability.State != "not_in_building" || len(z.Copies) != 0 || z.Wayfinding != nil {
		t.Fatalf("zero-copy %+v", z)
	}
	if _, err := svc.Detail(ctx, uuid.NewString(), uuid.Nil); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}
