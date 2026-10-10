// Package library implements the personal library (docs/03 §6.1–6.2).
package library

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Repository is the persistence the library needs (repository.LibraryRepository).
type Repository interface {
	BookAvailability(ctx context.Context, bookID uuid.UUID) (exists bool, available int, err error)
	HasEntry(ctx context.Context, userID, bookID uuid.UUID) (bool, error)
	Upsert(ctx context.Context, userID, bookID uuid.UUID, patch model.LibraryPatch, defaultStatus string) (model.LibraryEntry, bool, error)
	Delete(ctx context.Context, userID, bookID uuid.UUID) error
	List(ctx context.Context, userID uuid.UUID, f model.LibraryFilter, page, perPage int) ([]model.LibraryItem, int, error)
	Counts(ctx context.Context, userID uuid.UUID) (model.LibraryCounts, error)
}

// Holds lists a user's active waitlist rows (repository.WaitlistRepository).
type Holds interface {
	List(ctx context.Context, userID uuid.UUID) ([]model.WaitlistItem, error)
}

// Service is the library use-case layer.
type Service struct {
	repo  Repository
	holds Holds
}

// New builds the service; holds may be nil (waitlist inclusion disabled).
func New(repo Repository, holds Holds) *Service { return &Service{repo: repo, holds: holds} }

// MaxPerPage caps per_page (docs/03 §1).
const MaxPerPage = 50

var validStatus = map[string]bool{model.StatusWantToRead: true, model.StatusReading: true, model.StatusRead: true}

func invalid(format string, a ...any) error {
	return model.New(model.CodeValidationFailed, fmt.Sprintf(format, a...))
}

func notFound() error { return model.New(model.CodeNotFound, "Book not found.") }

func parseBook(id string) (uuid.UUID, error) {
	b, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, notFound()
	}
	return b, nil
}

// ValidatePatch checks field ranges (docs/03 §6.2).
func ValidatePatch(p model.LibraryPatch) error {
	if p.Status != nil && !validStatus[*p.Status] {
		return invalid("status must be want_to_read, reading or read.")
	}
	if p.ProgressPct.Set && !p.ProgressPct.Null && (p.ProgressPct.Value < 0 || p.ProgressPct.Value > 100) {
		return invalid("progress_pct must be between 0 and 100.")
	}
	if p.Rating.Set && !p.Rating.Null && (p.Rating.Value < 1 || p.Rating.Value > 5) {
		return invalid("rating must be between 1 and 5.")
	}
	return nil
}

// Add upserts an entry (POST /api/library). It returns created=true for a new row.
// A first-time `reading` on a book with zero available copies is a 409 unless force.
func (s *Service) Add(ctx context.Context, userID uuid.UUID, bookID string, status *string, force bool) (model.LibraryEntry, bool, error) {
	bid, err := parseBook(bookID)
	if err != nil {
		return model.LibraryEntry{}, false, err
	}
	patch := model.LibraryPatch{Status: status}
	if err := ValidatePatch(patch); err != nil {
		return model.LibraryEntry{}, false, err
	}
	exists, available, err := s.repo.BookAvailability(ctx, bid)
	if err != nil {
		return model.LibraryEntry{}, false, err
	}
	if !exists {
		return model.LibraryEntry{}, false, notFound()
	}
	if status != nil && *status == model.StatusReading && available == 0 && !force {
		already, err := s.repo.HasEntry(ctx, userID, bid)
		if err != nil {
			return model.LibraryEntry{}, false, err
		}
		if !already {
			return model.LibraryEntry{}, false, model.ErrBookUnavailable.WithExtra(map[string]any{
				"suggested_actions": []string{"reserve", "notify_when_available"},
			})
		}
	}
	return s.repo.Upsert(ctx, userID, bid, patch, model.StatusWantToRead)
}

// Patch updates an entry, creating it as want_to_read when absent (PATCH /api/library/{book_id}).
func (s *Service) Patch(ctx context.Context, userID uuid.UUID, bookID string, patch model.LibraryPatch) (model.LibraryEntry, error) {
	bid, err := parseBook(bookID)
	if err != nil {
		return model.LibraryEntry{}, err
	}
	if err := ValidatePatch(patch); err != nil {
		return model.LibraryEntry{}, err
	}
	exists, _, err := s.repo.BookAvailability(ctx, bid)
	if err != nil {
		return model.LibraryEntry{}, err
	}
	if !exists {
		return model.LibraryEntry{}, notFound()
	}
	e, _, err := s.repo.Upsert(ctx, userID, bid, patch, model.StatusWantToRead)
	return e, err
}

// Remove deletes an entry (idempotent).
func (s *Service) Remove(ctx context.Context, userID uuid.UUID, bookID string) error {
	bid, err := parseBook(bookID)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, userID, bid)
}

// ListRequest is GET /api/library input.
type ListRequest struct {
	Statuses        []string // want_to_read | reading | read | favorites
	IncludeWaitlist bool
	Page, PerPage   int
}

// Page is the GET /api/library response.
type Page struct {
	Items  []any               `json:"items"`
	Meta   model.PageMeta      `json:"meta"`
	Counts model.LibraryCounts `json:"counts"`
}

// List returns one page of entries (plus hold items on request) and the KPI counts.
func (s *Service) List(ctx context.Context, userID uuid.UUID, req ListRequest) (Page, error) {
	var f model.LibraryFilter
	for _, st := range req.Statuses {
		switch {
		case st == model.FilterFavorites:
			f.Favorites = true
		case validStatus[st]:
			f.Statuses = append(f.Statuses, st)
		default:
			return Page{}, invalid("status must be want_to_read, reading, read or favorites.")
		}
	}
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PerPage == 0 {
		req.PerPage = 20
	}
	if req.Page < 1 || req.PerPage < 1 || req.PerPage > MaxPerPage {
		return Page{}, invalid("page must be >= 1 and per_page between 1 and %d.", MaxPerPage)
	}
	entries, total, err := s.repo.List(ctx, userID, f, req.Page, req.PerPage)
	if err != nil {
		return Page{}, err
	}
	counts, err := s.repo.Counts(ctx, userID)
	if err != nil {
		return Page{}, err
	}
	items := make([]any, 0, len(entries))
	if req.IncludeWaitlist && s.holds != nil && req.Page == 1 {
		hs, err := s.holds.List(ctx, userID)
		if err != nil {
			return Page{}, err
		}
		for _, h := range hs {
			items = append(items, model.HoldItem{Kind: "hold", Book: h.Book, Waitlist: h.WaitlistEntry})
		}
	}
	for _, e := range entries {
		items = append(items, e)
	}
	pages := (total + req.PerPage - 1) / req.PerPage
	return Page{
		Items:  items,
		Meta:   model.PageMeta{Page: req.Page, PerPage: req.PerPage, Total: total, TotalPages: pages},
		Counts: counts,
	}, nil
}
