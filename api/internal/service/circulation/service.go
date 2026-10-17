// Package circulation implements patron self-service return, borrow, renew and loans
// list (docs/03 §6.4–6.7, docs/11 §2b).
package circulation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// Repository is the persistence the circulation service needs
// (implemented by repository.CirculationRepository).
type Repository interface {
	ReturnCopy(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.ReturnData, error)
	BorrowCopy(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.BorrowData, error)
	BorrowCopyForBook(ctx context.Context, actor, bookID uuid.UUID, now time.Time) (model.BorrowData, error)
	ReturnCopyForBook(ctx context.Context, actor, bookID uuid.UUID, now time.Time) (model.ReturnData, error)
	BookBorrowInfo(ctx context.Context, bookID, callerID uuid.UUID) (borrowable bool, borrowCopy *model.ScanBorrowCopy, returnable bool, myLoans []model.ScanMyLoan, err error)
	RenewLoan(ctx context.Context, actor, copyID uuid.UUID, now time.Time) (model.RenewData, error)
	ListLoans(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.LoanItem, model.LoansMeta, error)
	OpenLoanByCopy(ctx context.Context, copyID, callerID uuid.UUID) (*model.ScanLoanRef, error)
	CountOpenLoansByUser(ctx context.Context, userID uuid.UUID) (int, error)
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

// Return flips a checked_out copy back to the shelf (or onto a promoted hold) on behalf
// of actor, and closes the copy's open loan if one exists. Unknown or unparsable ids are
// NOT_FOUND; a copy that is not checked out is CONFLICT.
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
		Hold:       d.Hold,
		ClosedLoan: d.ClosedLoan,
	}
	if d.Location != nil {
		res.Copy.Location = catalog.LocationWayfinding(*d.Location)
	}
	return res, nil
}

// Borrow creates a loan for a copy. Unknown or unparsable ids are NOT_FOUND; an
// unavailable copy or wrong hold owner is CONFLICT; at the borrow limit is BORROW_LIMIT.
func (s *Service) Borrow(ctx context.Context, actor uuid.UUID, copyID string) (model.BorrowResult, error) {
	id, err := uuid.Parse(copyID)
	if err != nil {
		return model.BorrowResult{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	d, err := s.repo.BorrowCopy(ctx, actor, id, s.now().UTC())
	if err != nil {
		return model.BorrowResult{}, err
	}
	res := model.BorrowResult{
		Loan: d.Loan,
		Copy: model.ReturnedCopy{
			ID: d.Copy.ID, Barcode: d.Copy.Barcode, Status: d.Copy.Status, Condition: d.Copy.Condition,
			LastStateChange: d.Copy.LastStateChange,
		},
		Availability: model.DetailAvailability{
			TotalCopies:     d.Total,
			AvailableCopies: d.Available,
			State:           catalog.AvailabilityState(d.Total, d.Available, d.Statuses),
		},
	}
	if d.Location != nil {
		res.Copy.Location = catalog.LocationWayfinding(*d.Location)
	}
	return res, nil
}

// Renew extends an open loan. Unknown or unparsable ids are NOT_FOUND; no open loan or
// already renewed is CONFLICT; not the borrower is FORBIDDEN.
func (s *Service) Renew(ctx context.Context, actor uuid.UUID, copyID string) (model.RenewResult, error) {
	id, err := uuid.Parse(copyID)
	if err != nil {
		return model.RenewResult{}, model.New(model.CodeNotFound, "Copy not found.")
	}
	d, err := s.repo.RenewLoan(ctx, actor, id, s.now().UTC())
	if err != nil {
		return model.RenewResult{}, err
	}
	return model.RenewResult{
		Loan: model.RenewedLoan{
			ID:        d.ID,
			DueAt:     d.DueAt,
			Renewals:  d.Renewals,
			Renewable: d.Renewals < model.MaxRenewals,
		},
	}, nil
}

// BorrowBook picks a copy of the given book and borrows it (plan M21, docs/03 §6.8).
// The copy selection order: caller's reserved_waitlist copy first, else lowest barcode available.
// CONFLICT when no copy is borrowable; BORROW_LIMIT when at limit; NOT_FOUND for bad id.
func (s *Service) BorrowBook(ctx context.Context, actor uuid.UUID, bookID string) (model.BorrowResult, error) {
	bid, err := uuid.Parse(bookID)
	if err != nil {
		return model.BorrowResult{}, model.New(model.CodeNotFound, "Book not found.")
	}
	d, err := s.repo.BorrowCopyForBook(ctx, actor, bid, s.now().UTC())
	if err != nil {
		return model.BorrowResult{}, err
	}
	res := model.BorrowResult{
		Loan: d.Loan,
		Copy: model.ReturnedCopy{
			ID: d.Copy.ID, Barcode: d.Copy.Barcode, Status: d.Copy.Status, Condition: d.Copy.Condition,
			LastStateChange: d.Copy.LastStateChange,
		},
		Availability: model.DetailAvailability{
			TotalCopies:     d.Total,
			AvailableCopies: d.Available,
			State:           catalog.AvailabilityState(d.Total, d.Available, d.Statuses),
		},
	}
	if d.Location != nil {
		res.Copy.Location = catalog.LocationWayfinding(*d.Location)
	}
	return res, nil
}

// ReturnBook closes the caller's oldest open loan for any copy of the given book (plan M21, docs/03 §6.9).
// CONFLICT when no open loan; NOT_FOUND for bad id.
func (s *Service) ReturnBook(ctx context.Context, actor uuid.UUID, bookID string) (model.ReturnResult, error) {
	bid, err := uuid.Parse(bookID)
	if err != nil {
		return model.ReturnResult{}, model.New(model.CodeNotFound, "Book not found.")
	}
	d, err := s.repo.ReturnCopyForBook(ctx, actor, bid, s.now().UTC())
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
		Hold:       d.Hold,
		ClosedLoan: d.ClosedLoan,
	}
	if d.Location != nil {
		res.Copy.Location = catalog.LocationWayfinding(*d.Location)
	}
	return res, nil
}

// GetBookBorrowInfo returns the borrow_copy and my_loans fields for a scan book-match result (plan M21).
func (s *Service) GetBookBorrowInfo(ctx context.Context, bookID, callerID uuid.UUID) (
	borrowable bool, borrowCopy *model.ScanBorrowCopy, returnable bool, myLoans []model.ScanMyLoan, err error,
) {
	return s.repo.BookBorrowInfo(ctx, bookID, callerID)
}

// ListLoans returns the caller's loans, active first then past.
func (s *Service) ListLoans(ctx context.Context, userID uuid.UUID) (model.LoansListResult, error) {
	items, meta, err := s.repo.ListLoans(ctx, userID, s.now().UTC())
	if err != nil {
		return model.LoansListResult{}, err
	}
	return model.LoansListResult{Items: items, Meta: meta}, nil
}

// GetOpenLoan returns the open loan for a copy, annotated with mine=true when the caller
// is the borrower. Returns nil when there is no open loan.
func (s *Service) GetOpenLoan(ctx context.Context, copyID, callerID uuid.UUID) (*model.ScanLoanRef, error) {
	return s.repo.OpenLoanByCopy(ctx, copyID, callerID)
}

// CountOpenLoans returns the number of open loans for a user.
func (s *Service) CountOpenLoans(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountOpenLoansByUser(ctx, userID)
}
