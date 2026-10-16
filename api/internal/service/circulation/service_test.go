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

type fakeRepo struct {
	data            model.ReturnData
	err             error
	gotActor, gotID uuid.UUID
	gotNow          time.Time
	calls           int
}

func (f *fakeRepo) ReturnCopy(_ context.Context, actor, id uuid.UUID, now time.Time) (model.ReturnData, error) {
	f.calls++
	f.gotActor, f.gotID, f.gotNow = actor, id, now
	return f.data, f.err
}

func TestReturnMapsRepositoryResult(t *testing.T) {
	copyID, actor := uuid.New(), uuid.New()
	now := time.Date(2026, 10, 15, 9, 12, 0, 0, time.FixedZone("CST", 8*3600))
	repo := &fakeRepo{data: model.ReturnData{
		Copy: model.AdminCopy{ID: copyID, Barcode: "ATH-002941-C01", Status: model.CopyAvailable, Condition: "good", LastStateChange: now.UTC()},
		Location: &model.LocationRow{
			CopyID: copyID, BuildingCode: "MAIN", BuildingName: "Athenaeum Main Building", FloorNumber: 3,
			FloorName: "North Stacks", SectionCode: "SF", SectionName: "Science Fiction & Speculative",
			AisleCode: "A-1", ShelfCode: "S-2", RowLevel: 4, HeightTier: "Eye Level", Path: "MAIN/3F/SF/A-1/S-2",
		},
		Total: 3, Available: 3, Statuses: []string{model.CopyAvailable},
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
}

func TestReturnWithHoldTurnover(t *testing.T) {
	hold := &model.PromotedHold{ID: uuid.New(), Status: model.WaitReady, ExpiresAt: time.Now().Add(48 * time.Hour)}
	repo := &fakeRepo{data: model.ReturnData{
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
	repo.err = conflict
	if _, err := svc.Return(context.Background(), uuid.New(), uuid.NewString()); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("conflict passthrough: %v", err)
	}
}
