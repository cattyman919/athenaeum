// Package waitlist implements the holds queue (docs/03 §6.3) and its 48 h expiry sweep.
package waitlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Repository is the persistence the waitlist needs (repository.WaitlistRepository).
type Repository interface {
	Join(ctx context.Context, userID, bookID uuid.UUID) (model.WaitlistEntry, bool, error)
	Leave(ctx context.Context, userID, bookID uuid.UUID) error
	List(ctx context.Context, userID uuid.UUID) ([]model.WaitlistItem, error)
	Sweep(ctx context.Context, now time.Time) (model.SweepResult, error)
}

// Books answers availability questions (repository.LibraryRepository).
type Books interface {
	BookAvailability(ctx context.Context, bookID uuid.UUID) (exists bool, available int, err error)
}

// Service is the waitlist use-case layer.
type Service struct {
	repo  Repository
	books Books
}

// New builds the service.
func New(repo Repository, books Books) *Service { return &Service{repo: repo, books: books} }

func parseBook(id string) (uuid.UUID, error) {
	b, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, model.New(model.CodeNotFound, "Book not found.")
	}
	return b, nil
}

// Join queues the user for a book with no available copy. Joining twice returns the existing
// row (created=false); joining while copies are available is a CONFLICT.
func (s *Service) Join(ctx context.Context, userID uuid.UUID, bookID string) (model.WaitlistEntry, bool, error) {
	bid, err := parseBook(bookID)
	if err != nil {
		return model.WaitlistEntry{}, false, err
	}
	exists, available, err := s.books.BookAvailability(ctx, bid)
	if err != nil {
		return model.WaitlistEntry{}, false, err
	}
	if !exists {
		return model.WaitlistEntry{}, false, model.New(model.CodeNotFound, "Book not found.")
	}
	if available > 0 {
		return model.WaitlistEntry{}, false, model.New(model.CodeConflict, "Copies are available on the shelf; no waitlist is needed.")
	}
	return s.repo.Join(ctx, userID, bid)
}

// Leave cancels the user's active entry (idempotent).
func (s *Service) Leave(ctx context.Context, userID uuid.UUID, bookID string) error {
	bid, err := parseBook(bookID)
	if err != nil {
		return err
	}
	return s.repo.Leave(ctx, userID, bid)
}

// List returns the user's active holds.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]model.WaitlistItem, error) {
	return s.repo.List(ctx, userID)
}

// RunSweeper runs the expiry sweep every interval until ctx is done.
func (s *Service) RunSweeper(ctx context.Context, interval time.Duration, log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			res, err := s.repo.Sweep(ctx, now)
			if err != nil {
				log.Error("waitlist sweep failed", "err", err)
				continue
			}
			if res.Expired > 0 {
				log.Info("waitlist sweep", "expired", res.Expired, "promoted", res.Promoted, "released", res.Released)
			}
		}
	}
}
