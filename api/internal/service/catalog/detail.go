package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Ladder renders the prose ladder of docs/05 §3:
// "{building} ({floor_name}) → Floor {n} → {section} → Aisle {aisle} → Shelf {shelf} (Row {row}, {tier})".
func Ladder(l model.LocationRow) string {
	return fmt.Sprintf("%s (%s) → Floor %d → %s → Aisle %s → Shelf %s (Row %d, %s)",
		l.BuildingName, l.FloorName, l.FloorNumber, l.SectionName, l.AisleCode, l.ShelfCode, l.RowLevel, l.HeightTier)
}

// LadderSteps renders the turn-by-turn steps shown on the detail page.
func LadderSteps(l model.LocationRow) []string {
	return []string{
		fmt.Sprintf("Floor %d — %s", l.FloorNumber, l.FloorName),
		"Section: " + l.SectionName,
		"Aisle " + l.AisleCode,
		fmt.Sprintf("Shelf %s — Row %d, %s", l.ShelfCode, l.RowLevel, l.HeightTier),
	}
}

// Parts returns the structured location parts (docs/14 §6.1).
func Parts(l model.LocationRow) model.LocationParts {
	return model.LocationParts{
		BuildingCode: l.BuildingCode, BuildingName: l.BuildingName,
		FloorNumber: l.FloorNumber, FloorName: l.FloorName,
		SectionCode: l.SectionCode, SectionName: l.SectionName,
		AisleCode: l.AisleCode, ShelfCode: l.ShelfCode,
		RowLevel: l.RowLevel, HeightTier: l.HeightTier,
	}
}

// Detail assembles GET /api/books/{id}. An unparsable id is NOT_FOUND like any unknown id.
func (s *Service) Detail(ctx context.Context, id string, userID uuid.UUID) (model.BookDetail, error) {
	bid, err := uuid.Parse(id)
	if err != nil {
		return model.BookDetail{}, model.New(model.CodeNotFound, "Book not found.")
	}
	data, err := s.repo.Detail(ctx, bid, userID)
	if err != nil {
		return model.BookDetail{}, err
	}
	return assembleDetail(data), nil
}

// assembleDetail derives availability state, copy ladders and the home-shelf wayfinding
// (docs/03 §5.2) from the raw repository result; shared by Detail and Scan.
func assembleDetail(data model.BookDetailData) model.BookDetail {
	d := data.Detail
	d.Availability.State = AvailabilityState(d.Availability.TotalCopies, d.Availability.AvailableCopies, data.Statuses)

	// Home shelf: prefer an available copy that has a shelf, else any shelved copy.
	var home *model.LocationRow
	for i := range d.Copies {
		c := &d.Copies[i]
		if loc, ok := data.Locations[c.ID]; ok {
			c.Location = &model.CopyLocation{Path: loc.Path, Ladder: Ladder(loc), Parts: Parts(loc)}
			if home == nil || (c.Status == "available" && !isAvailable(d.Copies, home.CopyID)) {
				l := loc
				home = &l
			}
		}
	}
	if home != nil {
		d.Wayfinding = LocationWayfinding(*home)
	}
	return d
}

// LocationWayfinding renders a location as {path, parts, ladder_steps}.
func LocationWayfinding(l model.LocationRow) *model.Wayfinding {
	return &model.Wayfinding{Path: l.Path, Parts: Parts(l), LadderSteps: LadderSteps(l)}
}

func isAvailable(copies []model.CopyDetail, id uuid.UUID) bool {
	for _, c := range copies {
		if c.ID == id {
			return c.Status == "available"
		}
	}
	return false
}
