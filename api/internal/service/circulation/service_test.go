package circulation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
)

// fakeRepo implements circulation.Repository for unit tests.
type fakeRepo struct {
	// Return
	returnData      model.ReturnData
	returnErr       error
	gotActor, gotID uuid.UUID
	gotNow          time.Time
	calls           int
	// Borrow
	borrowData model.BorrowData
	borrowErr  error
	// Renew
	renewData model.RenewData
	renewErr  error
	// ListLoans
	loansItems []model.LoanItem
	loansMeta  model.LoansMeta
	loansErr   error
	// OpenLoanByCopy
	openLoan    *model.ScanLoanRef
	openLoanErr error
	// CountOpenLoansByUser
	loanCount    int
	loanCountErr error
}

func (f *fakeRepo) ReturnCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	f.calls++
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.returnData, f.returnErr
}
func (f *fakeRepo) BorrowCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.BorrowData, error) {
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.borrowData, f.borrowErr
}
func (f *fakeRepo) RenewLoan(_ context.Context, actor, id uuid.UUID, now time.Time) (model.RenewData, error) {
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.renewData, f.renewErr
}
func (f *fakeRepo) ListLoans(_ context.Context, _ uuid.UUID, _ time.Time) ([]model.LoanItem, model.LoansMeta, error) {
	return f.loansItems, f.loansMeta, f.loansErr
}
func (f *fakeRepo) OpenLoanByCopy(_ context.Context, _, _ uuid.UUID) (*model.ScanLoanRef, error) {
	return f.openLoan, f.openLoanErr
}
func (f *fakeRepo) CountOpenLoansByUser(_ context.Context, _ uuid.UUID) (int, error) {
	return f.loanCount, f.loanCountErr
}
func (f *fakeRepo) BorrowCopyForBook(_ context.Context, actor, id uuid.UUID, now time.Time) (model.BorrowData, error) {
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.borrowData, f.borrowErr
}
func (f *fakeRepo) ReturnCopyForBook(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	f.calls++
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.returnData, f.returnErr
}
func (f *fakeRepo) BookBorrowInfo(_ context.Context, _, _ uuid.UUID) (bool, *model.ScanBorrowCopy, bool, []model.ScanMyLoan, error) {
	return false, nil, false, []model.ScanMyLoan{}, nil
}

// ── Return ────────────────────────────────────────────────────────────────────

func TestReturnMapsRepositoryResult(t *testing.T) {
	copyID, actor := uuid.New(), uuid.New()
	now := time.Date(2026, 10, 15, 9, 12, 0, 0, time.FixedZone("CST", 8*3600))
	closedLoan := &model.ClosedLoanRef{ID: uuid.New(), ReturnedAt: now.UTC()}
	repo := &fakeRepo{returnData: model.ReturnData{
		Copy: model.AdminCopy{ID: copyID, Barcode: "ATH-002941-C01", Status: model.CopyAvailable, Condition: "good", LastStateChange: now.UTC()},
		Location: &model.LocationRow{
			CopyID: copyID, BuildingCode: "MAIN", BuildingName: "Athenaeum Main Building", FloorNumber: 3,
			FloorName: "North Stacks", SectionCode: "SF", SectionName: "Science Fiction & Speculative",
			AisleCode: "A-1", ShelfCode: "S-2", RowLevel: 4, HeightTier: "Eye Level", Path: "MAIN/3F/SF/A-1/S-2",
		},
		Total: 3, Available: 3, Statuses: []string{model.CopyAvailable},
		ClosedLoan: closedLoan,
	}}
	svc := circulation.New(repo).WithClock(func() time.Time { return now })

	res, err := svc.Return(context.Background(), actor, copyID.String())
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotActor != actor || repo.gotID != copyID || !repo.gotNow.Equal(now) || repo.gotNow.Location() != time.UTC {
		t.Fatalf("forwarded %v %v %v", repo.gotActor, repo.gotID, repo.gotNow)
	}
	if res.Copy.ID != copyID || res.Copy.Status != model.CopyAvailable || res.Copy.Barcode != "ATH-002941-C01" || !res.Copy.LastStateChange.Equal(now) {
		t.Fatalf("copy %+v", res.Copy)
	}
	if l := res.Copy.Location; l == nil || l.Path != "MAIN/3F/SF/A-1/S-2" || l.Parts.ShelfCode != "S-2" || len(l.LadderSteps) != 4 {
		t.Fatalf("location %+v", res.Copy.Location)
	}
	if res.Availability != (model.DetailAvailability{TotalCopies: 3, AvailableCopies: 3, State: model.StateAvailable}) || res.Hold != nil {
		t.Fatalf("availability %+v hold %+v", res.Availability, res.Hold)
	}
	if res.ClosedLoan != closedLoan {
		t.Fatalf("closed_loan %+v", res.ClosedLoan)
	}
}

func TestReturnWithHoldTurnover(t *testing.T) {
	hold := &model.PromotedHold{ID: uuid.New(), Status: model.WaitReady, ExpiresAt: time.Now().Add(48 * time.Hour)}
	repo := &fakeRepo{returnData: model.ReturnData{
		Copy:  model.AdminCopy{ID: uuid.New(), Status: model.CopyReservedWaitlist},
		Total: 1, Statuses: []string{model.CopyReservedWaitlist}, Hold: hold,
	}}
	res, err := circulation.New(repo).Return(context.Background(), uuid.New(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hold != hold || res.Copy.Location != nil || res.Availability.State != model.StateReserved {
		t.Fatalf("%+v", res)
	}
}

func TestReturnErrors(t *testing.T) {
	repo := &fakeRepo{}
	svc := circulation.New(repo)
	_, err := svc.Return(context.Background(), uuid.New(), "not-a-uuid")
	if !errors.Is(err, model.ErrNotFound) || repo.calls != 0 {
		t.Fatalf("bad id: %v (calls %d)", err, repo.calls)
	}
	conflict := model.New(model.CodeConflict, "Copy is not checked out").WithExtra(map[string]any{"current_status": "available"})
	repo.returnErr = conflict
	if _, err := svc.Return(context.Background(), uuid.New(), uuid.NewString()); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("conflict passthrough: %v", err)
	}
}

// ── Borrow ────────────────────────────────────────────────────────────────────

func TestBorrowMapsResult(t *testing.T) {
	copyID, actor := uuid.New(), uuid.New()
	loanID := uuid.New()
	now := time.Now().UTC()
	dueAt := now.Add(model.LoanPeriod)
	repo := &fakeRepo{borrowData: model.BorrowData{
		Loan: model.LoanDetail{
			ID: loanID, CopyID: copyID, UserID: actor, BorrowedAt: now,
			DueAt: dueAt, Renewals: 0, Renewable: true,
		},
		Copy:  model.AdminCopy{ID: copyID, Barcode: "ATH-002941-C01", Status: model.CopyCheckedOut, Condition: "good", LastStateChange: now},
		Total: 3, Available: 2, Statuses: []string{model.CopyAvailable, model.CopyCheckedOut},
	}}
	svc := circulation.New(repo).WithClock(func() time.Time { return now })

	res, err := svc.Borrow(context.Background(), actor, copyID.String())
	if err != nil {
		t.Fatal(err)
	}
	if res.Loan.ID != loanID || res.Loan.UserID != actor || !res.Loan.DueAt.Equal(dueAt) {
		t.Fatalf("loan %+v", res.Loan)
	}
	if res.Copy.Status != model.CopyCheckedOut || res.Availability.AvailableCopies != 2 {
		t.Fatalf("copy/avail %+v %+v", res.Copy, res.Availability)
	}
}

func TestBorrowInvalidID(t *testing.T) {
	repo := &fakeRepo{}
	_, err := circulation.New(repo).Borrow(context.Background(), uuid.New(), "bad-id")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want NOT_FOUND, got %v", err)
	}
}

func TestBorrowLimitError(t *testing.T) {
	repo := &fakeRepo{borrowErr: model.New(model.CodeBorrowLimit, "You already have 5 loans").
		WithExtra(map[string]any{"limit": 5})}
	_, err := circulation.New(repo).Borrow(context.Background(), uuid.New(), uuid.NewString())
	var de *model.Error
	if !errors.As(err, &de) || de.Code != model.CodeBorrowLimit {
		t.Fatalf("want BORROW_LIMIT, got %v", err)
	}
}

func TestBorrowConflictError(t *testing.T) {
	repo := &fakeRepo{borrowErr: model.New(model.CodeConflict, "Copy is not available to borrow").
		WithExtra(map[string]any{"current_status": "checked_out"})}
	_, err := circulation.New(repo).Borrow(context.Background(), uuid.New(), uuid.NewString())
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("want CONFLICT, got %v", err)
	}
}

// ── Renew ─────────────────────────────────────────────────────────────────────

func TestRenewMapsResult(t *testing.T) {
	loanID := uuid.New()
	copyID := uuid.New()
	now := time.Now().UTC()
	newDue := now.Add(model.LoanPeriod)
	repo := &fakeRepo{renewData: model.RenewData{ID: loanID, DueAt: newDue, Renewals: 1}}
	svc := circulation.New(repo).WithClock(func() time.Time { return now })

	res, err := svc.Renew(context.Background(), uuid.New(), copyID.String())
	if err != nil {
		t.Fatal(err)
	}
	if res.Loan.ID != loanID || !res.Loan.DueAt.Equal(newDue) || res.Loan.Renewals != 1 || res.Loan.Renewable {
		t.Fatalf("renew result %+v", res.Loan)
	}
}

func TestRenewInvalidID(t *testing.T) {
	repo := &fakeRepo{}
	_, err := circulation.New(repo).Renew(context.Background(), uuid.New(), "bad")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want NOT_FOUND, got %v", err)
	}
}

func TestRenewForbidden(t *testing.T) {
	repo := &fakeRepo{renewErr: model.New(model.CodeForbidden, "You may only renew your own loan.")}
	_, err := circulation.New(repo).Renew(context.Background(), uuid.New(), uuid.NewString())
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("want FORBIDDEN, got %v", err)
	}
}

func TestRenewAlreadyRenewed(t *testing.T) {
	repo := &fakeRepo{renewErr: model.New(model.CodeConflict, "Loan already renewed").WithExtra(map[string]any{"renewals": 1})}
	_, err := circulation.New(repo).Renew(context.Background(), uuid.New(), uuid.NewString())
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("want CONFLICT, got %v", err)
	}
}

// ── ListLoans ─────────────────────────────────────────────────────────────────

func TestListLoansMapsResult(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	items := []model.LoanItem{
		{Kind: "active", ID: uuid.New(), BorrowedAt: now, DueAt: now.Add(model.LoanPeriod), Renewals: 0, Renewable: true},
		{Kind: "past", ID: uuid.New(), BorrowedAt: past, DueAt: past.Add(model.LoanPeriod), ReturnedAt: &now, Renewals: 0, Renewable: false},
	}
	repo := &fakeRepo{
		loansItems: items,
		loansMeta:  model.LoansMeta{Active: 1, Overdue: 0},
	}
	res, err := circulation.New(repo).ListLoans(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || res.Meta.Active != 1 {
		t.Fatalf("loans %+v", res)
	}
	if res.Items[0].Kind != "active" || res.Items[1].Kind != "past" {
		t.Fatalf("kinds %s %s", res.Items[0].Kind, res.Items[1].Kind)
	}
}

func TestReturnClosesLoan(t *testing.T) {
	copyID := uuid.New()
	loanID := uuid.New()
	now := time.Now().UTC()
	closed := &model.ClosedLoanRef{ID: loanID, ReturnedAt: now}
	repo := &fakeRepo{returnData: model.ReturnData{
		Copy:  model.AdminCopy{ID: copyID, Status: model.CopyAvailable},
		Total: 1, Available: 1, Statuses: []string{model.CopyAvailable},
		ClosedLoan: closed,
	}}
	res, err := circulation.New(repo).Return(context.Background(), uuid.New(), copyID.String())
	if err != nil {
		t.Fatal(err)
	}
	if res.ClosedLoan == nil || res.ClosedLoan.ID != loanID {
		t.Fatalf("closed_loan %+v", res.ClosedLoan)
	}
}
