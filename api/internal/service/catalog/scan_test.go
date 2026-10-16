package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

const duneISBN = "9780441013593"

type scanRepo struct {
	bookID      uuid.UUID
	copies      []model.CopyDetail
	locations   map[uuid.UUID]model.LocationRow
	detailCalls int
	gotUser     uuid.UUID
}

func (r *scanRepo) BookIDByISBN(_ context.Context, isbn string) (uuid.UUID, bool, error) {
	if isbn == duneISBN {
		return r.bookID, true, nil
	}
	return uuid.Nil, false, nil
}

func (r *scanRepo) CopyRefByBarcode(_ context.Context, barcode string) (uuid.UUID, uuid.UUID, bool, error) {
	for _, c := range r.copies {
		if c.Barcode == barcode {
			return c.ID, r.bookID, true, nil
		}
	}
	return uuid.Nil, uuid.Nil, false, nil
}

func (r *scanRepo) Detail(_ context.Context, id, userID uuid.UUID) (model.BookDetailData, error) {
	r.detailCalls++
	r.gotUser = userID
	if id != r.bookID {
		return model.BookDetailData{}, model.New(model.CodeNotFound, "Book not found.")
	}
	isbn := duneISBN
	d := model.BookDetail{
		ID: id, Title: "Dune", Authors: []string{"Frank Herbert"}, ISBN13: &isbn,
		Section: &model.SectionRef{Code: "SF", Name: "Science Fiction & Speculative", Color: "#3B82F6"},
		Copies:  append([]model.CopyDetail(nil), r.copies...),
	}
	statuses := map[string]bool{}
	for _, c := range r.copies {
		d.Availability.TotalCopies++
		if c.Status == model.CopyAvailable {
			d.Availability.AvailableCopies++
		}
		statuses[c.Status] = true
	}
	data := model.BookDetailData{Detail: d, Locations: r.locations}
	for s := range statuses {
		data.Statuses = append(data.Statuses, s)
	}
	return data, nil
}

func loc(copyID uuid.UUID, shelf string) model.LocationRow {
	return model.LocationRow{
		CopyID: copyID, BuildingCode: "MAIN", BuildingName: "Athenaeum Main Building", FloorNumber: 3,
		FloorName: "North Stacks", SectionCode: "SF", SectionName: "Science Fiction & Speculative",
		AisleCode: "A-1", ShelfCode: shelf, RowLevel: 4, HeightTier: "Eye Level", Path: "MAIN/3F/SF/A-1/" + shelf,
	}
}

// Dune: C01 checked out on S-1, C02 available on S-2, C03 checked out without a shelf.
func newScanRepo() *scanRepo {
	c1, c2, c3 := uuid.New(), uuid.New(), uuid.New()
	return &scanRepo{
		bookID: uuid.New(),
		copies: []model.CopyDetail{
			{ID: c1, Barcode: "ATH-002941-C01", Status: model.CopyCheckedOut, Condition: "good"},
			{ID: c2, Barcode: "ATH-002941-C02", Status: model.CopyAvailable, Condition: "fair"},
			{ID: c3, Barcode: "ATH-002941-C03", Status: model.CopyCheckedOut, Condition: "good"},
		},
		locations: map[uuid.UUID]model.LocationRow{c1: loc(c1, "S-1"), c2: loc(c2, "S-2")},
	}
}

func TestScanISBNResolvesBookWithHomeShelf(t *testing.T) {
	repo := newScanRepo()
	svc := catalog.NewScanService(repo)
	res, err := svc.Scan(context.Background(), " 978-0-441-01359-3 ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != duneISBN || res.CodeType != model.ScanCodeISBN13 || !res.InCatalog || res.Match != model.ScanMatchBook {
		t.Fatalf("header %+v", res)
	}
	if res.Copy != nil || res.Returnable {
		t.Fatalf("book match must not carry a copy: %+v", res)
	}
	if res.Book == nil || res.Book.ID != repo.bookID || res.Book.Title != "Dune" || res.Book.Section.Code != "SF" || *res.Book.ISBN13 != duneISBN {
		t.Fatalf("book %+v", res.Book)
	}
	if a := res.Availability; a == nil || a.TotalCopies != 3 || a.AvailableCopies != 1 || a.State != model.StateAvailable {
		t.Fatalf("availability %+v", res.Availability)
	}
	// Home shelf = first available shelved copy (S-2), not the first shelved copy (S-1).
	if w := res.Wayfinding; w == nil || w.Path != "MAIN/3F/SF/A-1/S-2" || w.Parts.ShelfCode != "S-2" || len(w.LadderSteps) != 4 {
		t.Fatalf("wayfinding %+v", res.Wayfinding)
	}
	if repo.gotUser != uuid.Nil {
		t.Fatal("scan must not load a library entry")
	}
}

func TestScanCopyTag(t *testing.T) {
	repo := newScanRepo()
	svc := catalog.NewScanService(repo)

	res, err := svc.Scan(context.Background(), "ath-002941-c01")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "ATH-002941-C01" || res.CodeType != model.ScanCodeCopyBarcode || res.Match != model.ScanMatchCopy || !res.InCatalog {
		t.Fatalf("header %+v", res)
	}
	if !res.Returnable || res.Copy == nil || res.Copy.ID != repo.copies[0].ID || res.Copy.Status != model.CopyCheckedOut || res.Copy.Condition != "good" {
		t.Fatalf("copy %+v returnable=%v", res.Copy, res.Returnable)
	}
	// Wayfinding is that copy's own shelf, not the book's home shelf.
	if res.Wayfinding == nil || res.Wayfinding.Path != "MAIN/3F/SF/A-1/S-1" || res.Copy.Location == nil || res.Copy.Location.Path != res.Wayfinding.Path {
		t.Fatalf("location %+v / %+v", res.Copy.Location, res.Wayfinding)
	}
	if res.Book == nil || res.Book.ID != repo.bookID || res.Availability.AvailableCopies != 1 {
		t.Fatalf("book %+v avail %+v", res.Book, res.Availability)
	}

	res, _ = svc.Scan(context.Background(), "ATH-002941-C02")
	if res.Returnable || res.Copy.Status != model.CopyAvailable {
		t.Fatalf("available copy must not be returnable: %+v", res)
	}
	res, _ = svc.Scan(context.Background(), "ATH-002941-C03")
	if !res.Returnable || res.Copy.Location != nil || res.Wayfinding != nil {
		t.Fatalf("unshelved copy: %+v", res)
	}
}

func TestScanNoLocalMatch(t *testing.T) {
	repo := newScanRepo()
	svc := catalog.NewScanService(repo)
	for code, typ := range map[string]string{"9780306406157": model.ScanCodeISBN13, "ATH-999999-C01": model.ScanCodeCopyBarcode} {
		res, err := svc.Scan(context.Background(), code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if res.InCatalog || res.Match != model.ScanMatchNone || res.CodeType != typ || res.Book != nil || res.Copy != nil ||
			res.Availability != nil || res.Wayfinding != nil || res.Returnable {
			t.Fatalf("%s: %+v", code, res)
		}
	}
	if repo.detailCalls != 0 {
		t.Fatal("no detail load without a match")
	}
}

func TestScanRejectsInvalidCodes(t *testing.T) {
	svc := catalog.NewScanService(newScanRepo())
	for _, code := range []string{"9780441013590", "0441013595", "036000291452", "ATH-02941-C01", "ATH-002941-01", "hello"} {
		_, err := svc.Scan(context.Background(), code)
		var de *model.Error
		if !errors.As(err, &de) || de.Code != model.CodeInvalidISBN {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestNormalizeScanCode(t *testing.T) {
	tests := []struct{ in, code, typ string }{
		{"9780441013593", duneISBN, model.ScanCodeISBN13},
		{"978 0441 013593", duneISBN, model.ScanCodeISBN13},
		{"Ath-002941-C01", "ATH-002941-C01", model.ScanCodeCopyBarcode},
		{" ATH-002941 -C01 ", "ATH-002941-C01", model.ScanCodeCopyBarcode},
	}
	for _, tt := range tests {
		code, typ, err := catalog.NormalizeScanCode(tt.in)
		if err != nil || code != tt.code || typ != tt.typ {
			t.Errorf("%q → %q %q %v", tt.in, code, typ, err)
		}
	}
}
