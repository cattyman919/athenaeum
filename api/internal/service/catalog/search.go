// Package catalog implements tri-modal book search (docs/03 §5.1, docs/02 §7).
package catalog

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

const (
	// RRFK is the reciprocal-rank-fusion constant (docs/02 §7).
	RRFK = 60
	// VectorTopK is the number of books taken from the vector half (docs/02 §7).
	VectorTopK = 20
	// MaxPerPage caps per_page (docs/03 §1).
	MaxPerPage     = 50
	defaultPerPage = 20
)

// Repository is the persistence the catalog needs (repository.CatalogRepository).
type Repository interface {
	// Keyword returns every matching book ordered by relevance (all filtered
	// books ordered by title when q is empty).
	Keyword(ctx context.Context, f model.SearchFilters, q string) ([]model.SearchHit, error)
	// Semantic returns up to limit books ranked by best-chunk cosine similarity.
	Semantic(ctx context.Context, f model.SearchFilters, vec []float32, limit int) ([]model.SearchHit, error)
	// Summaries hydrates books (metadata, section, home shelf, copy aggregates).
	Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.BookSummary, error)
	// Detail loads one book with copies, locations, neighbors and the caller's library entry.
	Detail(ctx context.Context, id uuid.UUID, userID uuid.UUID) (model.BookDetailData, error)
}

// Embedder embeds search queries (embed.Ollama).
type Embedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// PendingCounter reports the auto-index backlog size (repository.IndexStore).
type PendingCounter interface {
	PendingCount(ctx context.Context) (int, error)
}

// Request is a validated-by-Service search request.
type Request struct {
	Query   string
	Mode    string
	Filters model.SearchFilters
	Sort    string
	Page    int
	PerPage int
}

// Service runs searches.
type Service struct {
	repo    Repository
	emb     Embedder
	pending PendingCounter
}

// New builds the service.
func New(repo Repository, emb Embedder) *Service { return &Service{repo: repo, emb: emb} }

// WithPendingCounter fills meta.index_pending on search responses (docs/03 §5.1);
// without one it stays 0.
func (s *Service) WithPendingCounter(p PendingCounter) *Service { s.pending = p; return s }

var (
	validSections = map[string]bool{"SF": true, "SCI": true, "HVA": true, "REF": true, "PER": true}
	validEras     = map[string]bool{"pre1800": true, "1800_1970": true, "1970_2000": true, "2000_n": true}
)

func invalid(format string, a ...any) error {
	return model.New(model.CodeValidationFailed, fmt.Sprintf(format, a...))
}

// Normalize validates and defaults a request.
func Normalize(r Request) (Request, error) {
	r.Query = strings.TrimSpace(r.Query)
	switch r.Mode {
	case "":
		r.Mode = model.ModeKeyword
	case model.ModeKeyword, model.ModeSemantic, model.ModeHybrid:
	default:
		return r, invalid("mode must be keyword, semantic or hybrid.")
	}
	if r.Query == "" && r.Mode != model.ModeKeyword {
		return r, invalid("q is required for %s search.", r.Mode)
	}
	for _, s := range r.Filters.Sections {
		if !validSections[s] {
			return r, invalid("unknown section %q.", s)
		}
	}
	for _, e := range r.Filters.Eras {
		if !validEras[e] {
			return r, invalid("unknown era %q.", e)
		}
	}
	switch r.Sort {
	case "":
		r.Sort = "relevance"
	case "relevance", "title", "year:asc", "year:desc":
	default:
		return r, invalid("sort must be relevance, title, year:asc or year:desc.")
	}
	if r.Page == 0 {
		r.Page = 1
	}
	if r.PerPage == 0 {
		r.PerPage = defaultPerPage
	}
	if r.Page < 1 || r.PerPage < 1 || r.PerPage > MaxPerPage {
		return r, invalid("page must be >= 1 and per_page between 1 and %d.", MaxPerPage)
	}
	return r, nil
}

// Search executes the request.
func (s *Service) Search(ctx context.Context, req Request) (model.SearchPage, error) {
	req, err := Normalize(req)
	if err != nil {
		return model.SearchPage{}, err
	}
	hits, err := s.rank(ctx, req)
	if err != nil {
		return model.SearchPage{}, err
	}

	// Sorting other than relevance needs the metadata; hydrate all hits (ids are few).
	summaries, err := s.repo.Summaries(ctx, hitIDs(hits))
	if err != nil {
		return model.SearchPage{}, err
	}
	items := make([]model.BookSummary, 0, len(hits))
	for _, h := range hits {
		b, ok := summaries[h.BookID]
		if !ok {
			continue
		}
		b.Score = h.Score
		b.Availability.State = AvailabilityState(b.Availability.TotalCopies, b.Availability.AvailableCopies, b.Statuses)
		items = append(items, b)
	}
	sortItems(items, req)

	indexPending := 0
	if s.pending != nil {
		if indexPending, err = s.pending.PendingCount(ctx); err != nil {
			return model.SearchPage{}, err
		}
	}

	total := len(items)
	pages := (total + req.PerPage - 1) / req.PerPage
	start := min((req.Page-1)*req.PerPage, total)
	end := min(start+req.PerPage, total)
	return model.SearchPage{
		Items: items[start:end],
		Meta: model.SearchMeta{
			PageMeta:     model.PageMeta{Page: req.Page, PerPage: req.PerPage, Total: total, TotalPages: pages},
			IndexPending: indexPending,
		},
	}, nil
}

func (s *Service) rank(ctx context.Context, req Request) ([]model.SearchHit, error) {
	switch req.Mode {
	case model.ModeKeyword:
		return s.repo.Keyword(ctx, req.Filters, req.Query)
	case model.ModeSemantic:
		vec, err := s.emb.EmbedQuery(ctx, req.Query)
		if err != nil {
			return nil, err
		}
		return s.repo.Semantic(ctx, req.Filters, vec, VectorTopK)
	default: // hybrid
		vec, err := s.emb.EmbedQuery(ctx, req.Query)
		if err != nil {
			return nil, err
		}
		vh, err := s.repo.Semantic(ctx, req.Filters, vec, VectorTopK)
		if err != nil {
			return nil, err
		}
		kh, err := s.repo.Keyword(ctx, req.Filters, req.Query)
		if err != nil {
			return nil, err
		}
		if len(kh) > VectorTopK {
			kh = kh[:VectorTopK]
		}
		return FuseRRF(kh, vh), nil
	}
}

// FuseRRF merges ranked lists with reciprocal rank fusion, k=60 (docs/02 §7):
// score = Σ 1/(60+rank). Books present in either list are returned.
func FuseRRF(lists ...[]model.SearchHit) []model.SearchHit {
	score := map[uuid.UUID]float64{}
	var order []uuid.UUID
	for _, l := range lists {
		for i, h := range l {
			if _, seen := score[h.BookID]; !seen {
				order = append(order, h.BookID)
			}
			score[h.BookID] += 1.0 / float64(RRFK+i+1)
		}
	}
	out := make([]model.SearchHit, 0, len(order))
	for _, id := range order {
		out = append(out, model.SearchHit{BookID: id, Score: score[id]})
	}
	slices.SortStableFunc(out, func(a, b model.SearchHit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	return out
}

// AvailabilityState resolves availability.state (docs/03 §5.1).
func AvailabilityState(total, available int, statuses []string) string {
	switch {
	case available > 0:
		return model.StateAvailable
	case total == 0:
		return model.StateNotInBuilding
	case len(statuses) == 1:
		return statuses[0]
	default:
		return model.StateCheckedOut
	}
}

func hitIDs(hits []model.SearchHit) []uuid.UUID {
	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.BookID
	}
	return ids
}

func sortItems(items []model.BookSummary, req Request) {
	sort := req.Sort
	if sort == "relevance" && req.Query == "" {
		sort = "title" // relevance is only meaningful for search results
	}
	byTitle := func(a, b model.BookSummary) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	}
	switch sort {
	case "title":
		slices.SortStableFunc(items, byTitle)
	case "year:asc", "year:desc":
		desc := sort == "year:desc"
		slices.SortStableFunc(items, func(a, b model.BookSummary) int {
			switch {
			case a.PublishedYear == nil && b.PublishedYear == nil:
				return byTitle(a, b)
			case a.PublishedYear == nil:
				return 1
			case b.PublishedYear == nil:
				return -1
			case *a.PublishedYear == *b.PublishedYear:
				return byTitle(a, b)
			case (*a.PublishedYear < *b.PublishedYear) != desc:
				return -1
			default:
				return 1
			}
		})
	}
	// relevance: keep the ranked order from the repository / RRF.
}
