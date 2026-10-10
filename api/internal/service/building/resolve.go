// Package building resolves location path tokens to shelves (docs/05 §3–4).
package building

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Path is a parsed "MAIN/3F/SF/A-1/S-2" token.
type Path struct {
	Building string
	Floor    int
	Section  string
	Aisle    string
	Shelf    string
}

// String renders the canonical token.
func (p Path) String() string {
	return fmt.Sprintf("%s/%dF/%s/%s/%s", p.Building, p.Floor, p.Section, p.Aisle, p.Shelf)
}

// ParsePath splits and validates a path token; malformed input is a
// VALIDATION_FAILED error.
func ParsePath(s string) (Path, error) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	bad := func() (Path, error) {
		return Path{}, model.New(model.CodeValidationFailed, fmt.Sprintf("Invalid location path %q (expected MAIN/3F/SF/A-1/S-2).", s))
	}
	if len(parts) != 5 {
		return bad()
	}
	for _, p := range parts {
		if p == "" {
			return bad()
		}
	}
	fl := parts[1]
	if len(fl) < 2 || fl[len(fl)-1] != 'F' {
		return bad()
	}
	n, err := strconv.Atoi(fl[:len(fl)-1])
	if err != nil || n < 0 {
		return bad()
	}
	return Path{Building: parts[0], Floor: n, Section: parts[2], Aisle: parts[3], Shelf: parts[4]}, nil
}

// Lookup resolves one hierarchy level; each method returns model.ErrNotFound-coded
// errors when the row is absent. Implemented by repository.BuildingRepository.
type Lookup interface {
	Building(ctx context.Context, code string) (uuid.UUID, error)
	Floor(ctx context.Context, buildingID uuid.UUID, number int) (uuid.UUID, error)
	Section(ctx context.Context, floorID uuid.UUID, code string) (uuid.UUID, error)
	Aisle(ctx context.Context, sectionID uuid.UUID, code string) (uuid.UUID, error)
	Shelf(ctx context.Context, aisleID uuid.UUID, code string) (uuid.UUID, error)
}

// ResolvePath maps a path token to a shelf id, failing with NOT_FOUND naming
// the first missing segment (docs/05 §4).
func ResolvePath(ctx context.Context, l Lookup, path string) (uuid.UUID, error) {
	p, err := ParsePath(path)
	if err != nil {
		return uuid.Nil, err
	}
	notFound := func(level string) error {
		return model.New(model.CodeNotFound, fmt.Sprintf("Location %q not found (no such %s).", path, level))
	}
	step := func(level string, id uuid.UUID, err error) (uuid.UUID, error) {
		if err != nil {
			if de, ok := model.AsError(err); ok && de.Code == model.CodeNotFound {
				return uuid.Nil, notFound(level)
			}
			return uuid.Nil, err
		}
		return id, nil
	}
	b, err := l.Building(ctx, p.Building)
	if b, err = step("building", b, err); err != nil {
		return uuid.Nil, err
	}
	f, err := l.Floor(ctx, b, p.Floor)
	if f, err = step("floor", f, err); err != nil {
		return uuid.Nil, err
	}
	s, err := l.Section(ctx, f, p.Section)
	if s, err = step("section", s, err); err != nil {
		return uuid.Nil, err
	}
	a, err := l.Aisle(ctx, s, p.Aisle)
	if a, err = step("aisle", a, err); err != nil {
		return uuid.Nil, err
	}
	sh, err := l.Shelf(ctx, a, p.Shelf)
	return step("shelf", sh, err)
}
