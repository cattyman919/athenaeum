// Package circulation implements patron self-service return (docs/03 §6.4, docs/11 §2b).
package circulation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// Repository is the persistence a return needs (repository.CirculationRepository).
type Repository interface {
	ReturnCopy(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.ReturnData, error)
}

// Service is the circulation use-case layer.
type Service struct {
	repo Repository
	now  func() time.Time
}

// New builds the service.
func New(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Return flips a checked_out copy back to the shelf (or onto a promoted hold) on behalf of
// actor. Unknown or unparsable ids are NOT_FOUND; a copy that is not checked out is CONFLICT.
func (s *Service) Return(ctx context.Context, actor uuid.UUID, copyID string) (model.ReturnResult, error) {
	id, err := uuid.Parse(copyID)
	if err != nil {
		return model.ReturnResult{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	d, err := s.repo.ReturnCopy(ctx, actor, id, s.now().UTC())
	if err != nil {
		return model.ReturnResult{}, err
	}
	res := model.ReturnResult{
		Copy: model.ReturnedCopy{
			ID: d.Copy.ID, Barcode: d.Copy.Barcode, Status: d.Copy.Status, Condition: d.Copy.Condition,
			LastStateChange: d.Copy.LastStateChange,
		},
		Availability: model.DetailAvailability{
			TotalCopies:     d.Total,
			AvailableCopies: d.Available,
			State:           catalog.AvailabilityState(d.Total, d.Available, d.Statuses),
		},
		Hold: d.Hold,
	}
	if d.Location != nil {
		res.Copy.Location = catalog.LocationWayfinding(*d.Location)
	}
	return res, nil
}
