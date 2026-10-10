// Package seed loads, validates and applies the mock fixtures (docs/08).
package seed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/checkdigit"
	"github.com/cattyman919/athenaeum/api/internal/service/building"
)

// SectionColors are the canonical wayfinding colors (docs/05 §2).
var SectionColors = map[string]string{
	"SCI": "#10B981", "PER": "#F59E0B", "HVA": "#8B5CF6", "REF": "#6B7280", "SF": "#3B82F6",
}

// AccessionBase: book #n gets accession 002940+n (docs/08 §5).
const AccessionBase = 2940

var barcodeRe = regexp.MustCompile(`^ATH-[0-9]{6}-C[0-9]{2}$`)

var validStatus = map[string]bool{"available": true, "checked_out": true, "missing_lost": true}

// Fixtures is the parsed mocks directory.
type Fixtures struct {
	Building model.BuildingFixture
	Books    []model.BookFixture
}

// Barcode builds the accession barcode of copy n (1-based) of book index (1-based).
func Barcode(bookIndex, copyN int) string {
	return fmt.Sprintf("ATH-%06d-C%02d", AccessionBase+bookIndex, copyN)
}

// Load reads building.json and books.json from dir, rejecting unknown fields.
func Load(dir string) (Fixtures, error) {
	var fx Fixtures
	var books model.BooksFixture
	if err := readJSON(filepath.Join(dir, "building.json"), &fx.Building); err != nil {
		return Fixtures{}, err
	}
	if err := readJSON(filepath.Join(dir, "books.json"), &books); err != nil {
		return Fixtures{}, err
	}
	fx.Books = books.Books
	return fx, nil
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path) //nolint:gosec // path is the operator-supplied mocks dir
	if err != nil {
		return fmt.Errorf("seed: read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("seed: parse %s: %w", path, err)
	}
	return nil
}

// Validate enforces every hard-failure rule of docs/08 §8 that can be checked
// without a database. Errors name the offending entry (bad fixture, not code).
func (f Fixtures) Validate() error {
	paths := map[string]bool{}
	sections := map[string]bool{}
	if f.Building.Building.Code == "" {
		return fmt.Errorf("building.json: building.code is required")
	}
	for fi, fl := range f.Building.Floors {
		for si, sec := range fl.Sections {
			want, known := SectionColors[sec.Code]
			if !known {
				return fmt.Errorf("building.json: floors[%d].sections[%d]: unknown section code %q", fi, si, sec.Code)
			}
			if sec.Color != want {
				return fmt.Errorf("building.json: section %s color %q does not match canonical %q", sec.Code, sec.Color, want)
			}
			sections[sec.Code] = true
			for _, ai := range sec.Aisles {
				for _, sh := range ai.Shelves {
					p := building.Path{Building: f.Building.Building.Code, Floor: fl.FloorNumber, Section: sec.Code, Aisle: ai.Code, Shelf: sh.Code}
					if paths[p.String()] {
						return fmt.Errorf("building.json: duplicate shelf %s", p)
					}
					paths[p.String()] = true
				}
			}
		}
	}

	barcodes := map[string]bool{}
	isbns := map[string]bool{}
	for i, b := range f.Books {
		idx := i + 1
		where := fmt.Sprintf("books.json: books[%d] (%s)", i, b.Title)
		if b.Title == "" {
			return fmt.Errorf("%s: title is required", where)
		}
		if b.ISBN13 != "" {
			if !checkdigit.ValidISBN13(b.ISBN13) {
				return fmt.Errorf("%s: invalid ISBN-13 checkdigit %q", where, b.ISBN13)
			}
			if isbns[b.ISBN13] {
				return fmt.Errorf("%s: duplicate ISBN-13 %q", where, b.ISBN13)
			}
			isbns[b.ISBN13] = true
		}
		if !sections[b.SectionCode] {
			return fmt.Errorf("%s: unknown section_code %q", where, b.SectionCode)
		}
		for n, c := range b.Copies {
			cw := fmt.Sprintf("%s copies[%d]", where, n)
			if !validStatus[c.Status] {
				return fmt.Errorf("%s: invalid status %q", cw, c.Status)
			}
			if c.Barcode != "" {
				if !barcodeRe.MatchString(c.Barcode) {
					return fmt.Errorf("%s: malformed barcode %q", cw, c.Barcode)
				}
				if want := Barcode(idx, n+1); c.Barcode != want {
					return fmt.Errorf("%s: barcode drift: got %s, accession counter expects %s", cw, c.Barcode, want)
				}
			}
			bc := c.Barcode
			if bc == "" {
				bc = Barcode(idx, n+1)
			}
			if barcodes[bc] {
				return fmt.Errorf("%s: duplicate barcode %s", cw, bc)
			}
			barcodes[bc] = true
			p, err := building.ParsePath(c.ShelfPath)
			if err != nil {
				return fmt.Errorf("%s: %w", cw, err)
			}
			if !paths[p.String()] {
				return fmt.Errorf("%s: unknown shelf_path %q", cw, c.ShelfPath)
			}
		}
	}
	return nil
}

// Counts returns the fixture totals (compare with docs/13 §1).
func (f Fixtures) Counts() model.SeedCounts {
	var c model.SeedCounts
	for _, fl := range f.Building.Floors {
		for _, s := range fl.Sections {
			for _, a := range s.Aisles {
				c.Shelves += len(a.Shelves)
			}
		}
	}
	for _, b := range f.Books {
		c.Books++
		c.Copies += len(b.Copies)
		if len(b.Copies) == 0 {
			c.ZeroCopyBooks++
		}
		for _, cp := range b.Copies {
			if cp.Status == "checked_out" {
				c.CheckedOut++
			}
		}
	}
	return c
}
