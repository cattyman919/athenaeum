package building_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/building"
)

// fakeLookup knows exactly one shelf: MAIN/3F/SF/A-1/S-2.
type fakeLookup struct{ ids map[string]uuid.UUID }

func newFake() *fakeLookup {
	f := &fakeLookup{ids: map[string]uuid.UUID{}}
	for _, k := range []string{"b", "f", "s", "a", "sh"} {
		f.ids[k] = uuid.New()
	}
	return f
}

func nf() error { return model.New(model.CodeNotFound, "x") }

func (f *fakeLookup) Building(_ context.Context, c string) (uuid.UUID, error) {
	if c == "MAIN" {
		return f.ids["b"], nil
	}
	return uuid.Nil, nf()
}

func (f *fakeLookup) Floor(_ context.Context, b uuid.UUID, n int) (uuid.UUID, error) {
	if b == f.ids["b"] && n == 3 {
		return f.ids["f"], nil
	}
	return uuid.Nil, nf()
}

func (f *fakeLookup) Section(_ context.Context, fl uuid.UUID, c string) (uuid.UUID, error) {
	if fl == f.ids["f"] && c == "SF" {
		return f.ids["s"], nil
	}
	return uuid.Nil, nf()
}

func (f *fakeLookup) Aisle(_ context.Context, s uuid.UUID, c string) (uuid.UUID, error) {
	if s == f.ids["s"] && c == "A-1" {
		return f.ids["a"], nil
	}
	return uuid.Nil, nf()
}

func (f *fakeLookup) Shelf(_ context.Context, a uuid.UUID, c string) (uuid.UUID, error) {
	if a == f.ids["a"] && c == "S-2" {
		return f.ids["sh"], nil
	}
	return uuid.Nil, nf()
}

func TestParsePath(t *testing.T) {
	p, err := building.ParsePath("MAIN/3F/SF/A-1/S-2")
	if err != nil || p != (building.Path{Building: "MAIN", Floor: 3, Section: "SF", Aisle: "A-1", Shelf: "S-2"}) {
		t.Fatalf("%+v %v", p, err)
	}
	if p.String() != "MAIN/3F/SF/A-1/S-2" {
		t.Fatalf("round trip %s", p)
	}
	if p, err := building.ParsePath("MAIN/0F/LOB/A-1/S-1"); err != nil || p.Floor != 0 {
		t.Fatalf("lobby floor: %+v %v", p, err)
	}
	for _, bad := range []string{"", "MAIN", "MAIN/3F/SF/A-1", "MAIN/3F/SF/A-1/S-2/X", "MAIN/3/SF/A-1/S-2", "MAIN/xF/SF/A-1/S-2", "MAIN//SF/A-1/S-2", "MAIN/-1F/SF/A-1/S-2"} {
		_, err := building.ParsePath(bad)
		var de *model.Error
		if !errors.As(err, &de) || de.Code != model.CodeValidationFailed {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestResolvePath(t *testing.T) {
	f := newFake()
	id, err := building.ResolvePath(context.Background(), f, "MAIN/3F/SF/A-1/S-2")
	if err != nil || id != f.ids["sh"] {
		t.Fatalf("%v %v", id, err)
	}
	tests := map[string]string{
		"XXXX/3F/SF/A-1/S-2": "building",
		"MAIN/9F/SF/A-1/S-2": "floor",
		"MAIN/3F/ZZ/A-1/S-2": "section",
		"MAIN/3F/SF/A-9/S-2": "aisle",
		"MAIN/3F/SF/A-1/S-9": "shelf",
	}
	for path, level := range tests {
		_, err := building.ResolvePath(context.Background(), f, path)
		var de *model.Error
		if !errors.As(err, &de) || de.Code != model.CodeNotFound || !strings.Contains(de.Message, "no such "+level) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if _, err := building.ResolvePath(context.Background(), f, "garbage"); err == nil {
		t.Fatal("garbage must fail")
	}
}
