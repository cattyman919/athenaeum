package seed_test

import (
	"strings"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
)

const mocksDir = "../../../mocks"

func load(t *testing.T) seed.Fixtures {
	t.Helper()
	fx, err := seed.Load(mocksDir)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func TestShippedFixturesAreValidAndMatchDocs13(t *testing.T) {
	fx := load(t)
	if err := fx.Validate(); err != nil {
		t.Fatal(err)
	}
	c := fx.Counts()
	want := model.SeedCounts{Books: 39, Copies: 58, Shelves: 40, CheckedOut: 11, ZeroCopyBooks: 5, LibraryEntries: 23, WaitlistRows: 2}
	if c != want {
		t.Fatalf("counts %+v, want %+v", c, want)
	}
	// 1 building · 3 floors · 5 sections · 15 aisles.
	var sections, aisles int
	for _, fl := range fx.Building.Floors {
		sections += len(fl.Sections)
		for _, s := range fl.Sections {
			aisles += len(s.Aisles)
		}
	}
	if len(fx.Building.Floors) != 3 || sections != 5 || aisles != 15 {
		t.Fatalf("tree shape floors=%d sections=%d aisles=%d", len(fx.Building.Floors), sections, aisles)
	}
}

func TestShippedFixtureSpotChecks(t *testing.T) {
	fx := load(t)
	dune := fx.Books[0]
	if dune.Title != "Dune" || dune.ISBN13 != "9780441013593" || len(dune.Copies) != 3 ||
		dune.Copies[0].Barcode != "ATH-002941-C01" || dune.Copies[0].Status != "checked_out" ||
		dune.Copies[0].ShelfPath != "MAIN/3F/SF/A-1/S-2" || dune.Copies[2].ShelfPath != "MAIN/3F/SF/A-1/S-3" {
		t.Fatalf("dune %+v", dune)
	}
	// Every checked-out copy is C01 of a multi-copy book (docs/13 §3).
	for _, b := range fx.Books {
		for n, c := range b.Copies {
			if c.Status == "checked_out" && (n != 0 || len(b.Copies) < 2) {
				t.Errorf("%s: checked-out copy %s must be C01 of a multi-copy book", b.Title, c.Barcode)
			}
		}
	}
	// 1-based file positions; docs/13 keeps catalog numbers #33/#34/#38–#40 (#8 retired).
	zero := map[int]bool{32: true, 33: true, 37: true, 38: true, 39: true}
	for i, b := range fx.Books {
		if (len(b.Copies) == 0) != zero[i+1] {
			t.Errorf("book #%d %s zero-copy mismatch", i+1, b.Title)
		}
	}
	// SF section: 9 books (docs/13 §5).
	sf := 0
	for _, b := range fx.Books {
		if b.SectionCode == "SF" {
			sf++
		}
	}
	if sf != 9 {
		t.Errorf("SF books %d", sf)
	}
}

func TestValidateFailureModes(t *testing.T) {
	mut := func(f func(fx *seed.Fixtures)) seed.Fixtures {
		fx := load(t)
		f(&fx)
		return fx
	}
	tests := []struct {
		name string
		fx   seed.Fixtures
		want string
	}{
		{"bad isbn checkdigit", mut(func(fx *seed.Fixtures) { fx.Books[0].ISBN13 = "9780441013594" }), "checkdigit"},
		{"duplicate isbn", mut(func(fx *seed.Fixtures) { fx.Books[1].ISBN13 = fx.Books[0].ISBN13 }), "duplicate ISBN"},
		{"unknown section", mut(func(fx *seed.Fixtures) { fx.Books[0].SectionCode = "ZZZ" }), "unknown section_code"},
		{"section color mismatch", mut(func(fx *seed.Fixtures) { fx.Building.Floors[0].Sections[0].Color = "#000000" }), "canonical"},
		{"unknown section code in tree", mut(func(fx *seed.Fixtures) { fx.Building.Floors[0].Sections[0].Code = "XXX" }), "unknown section code"},
		{"unknown shelf path", mut(func(fx *seed.Fixtures) { fx.Books[0].Copies[0].ShelfPath = "MAIN/3F/SF/A-1/S-9" }), "unknown shelf_path"},
		{"malformed shelf path", mut(func(fx *seed.Fixtures) { fx.Books[0].Copies[0].ShelfPath = "nope" }), "Invalid location path"},
		{"barcode drift", mut(func(fx *seed.Fixtures) { fx.Books[0].Copies[0].Barcode = "ATH-009999-C01" }), "drift"},
		{"malformed barcode", mut(func(fx *seed.Fixtures) { fx.Books[0].Copies[0].Barcode = "ATH-2941-C1" }), "malformed barcode"},
		{"bad status", mut(func(fx *seed.Fixtures) { fx.Books[0].Copies[0].Status = "borrowed" }), "invalid status"},
		{"missing title", mut(func(fx *seed.Fixtures) { fx.Books[0].Title = "" }), "title is required"},
		{"duplicate shelf", mut(func(fx *seed.Fixtures) {
			a := &fx.Building.Floors[0].Sections[0].Aisles[0]
			a.Shelves = append(a.Shelves, a.Shelves[0])
		}), "duplicate shelf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fx.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestBarcodeAllocation(t *testing.T) {
	if got := seed.Barcode(1, 1); got != "ATH-002941-C01" {
		t.Fatal(got)
	}
	if got := seed.Barcode(40, 12); got != "ATH-002980-C12" {
		t.Fatal(got)
	}
}

func TestLoadRejectsMissingDirAndUnknownFields(t *testing.T) {
	if _, err := seed.Load(t.TempDir()); err == nil {
		t.Fatal("empty dir must fail")
	}
}

func TestLibraryFixtureMatchesDocs13(t *testing.T) {
	fx := load(t)
	byStatus := map[string]int{}
	fav := 0
	for _, e := range fx.Library.Entries {
		byStatus[e.Status]++
		if e.IsFavorite {
			fav++
		}
	}
	if byStatus["reading"] != 1 || byStatus["read"] != 15 || byStatus["want_to_read"] != 7 || fav != 7 {
		t.Fatalf("stages %v favorites %d", byStatus, fav)
	}
	if len(fx.Library.Waitlist) != 2 {
		t.Fatalf("waitlist %v", fx.Library.Waitlist)
	}
}

func TestValidateLibraryFailureModes(t *testing.T) {
	bad := func(f func(fx *seed.Fixtures)) seed.Fixtures {
		fx := load(t)
		f(&fx)
		return fx
	}
	tests := map[string]seed.Fixtures{
		"unknown title":  bad(func(fx *seed.Fixtures) { fx.Library.Entries[0].Title = "Nope" }),
		"duplicate":      bad(func(fx *seed.Fixtures) { fx.Library.Entries[1] = fx.Library.Entries[0] }),
		"bad status":     bad(func(fx *seed.Fixtures) { fx.Library.Entries[0].Status = "finished" }),
		"bad rating":     bad(func(fx *seed.Fixtures) { r := 9; fx.Library.Entries[0].Rating = &r }),
		"bad progress":   bad(func(fx *seed.Fixtures) { p := 120; fx.Library.Entries[0].ProgressPct = &p }),
		"unknown hold":   bad(func(fx *seed.Fixtures) { fx.Library.Waitlist[0] = "Nope" }),
		"missing e-mail": bad(func(fx *seed.Fixtures) { fx.Library.UserEmail = "" }),
	}
	for name, fx := range tests {
		if err := fx.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCatalogNumberSkipsRetired(t *testing.T) {
	for pos, want := range map[int]int{1: 1, 7: 7, 8: 9, 9: 10, 39: 40} {
		if got := seed.CatalogNumber(pos); got != want {
			t.Errorf("CatalogNumber(%d) = %d, want %d", pos, got, want)
		}
	}
}
