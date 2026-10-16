package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// copyTagRe is the Athenaeum copy tag shape (docs/11 §9).
var copyTagRe = regexp.MustCompile(`^ATH-[0-9]{6}-C[0-9]{2}$`)

// ScanRepository resolves scanned codes against local rows (repository.CatalogRepository).
type ScanRepository interface {
	BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error)
	CopyRefByBarcode(ctx context.Context, barcode string) (copyID, bookID uuid.UUID, found bool, err error)
	Detail(ctx context.Context, id uuid.UUID, userID uuid.UUID) (model.BookDetailData, error)
}

// ScanService implements POST /api/books/scan (docs/03 §5.4): a local-only resolver.
type ScanService struct {
	repo ScanRepository
}

// NewScanService builds the patron scan resolver.
func NewScanService(repo ScanRepository) *ScanService { return &ScanService{repo: repo} }

// NormalizeScanCode classifies a scanned payload. A copy tag is uppercased (spaces dropped)
// and matched against ^ATH-[0-9]{6}-C[0-9]{2}$; anything else (including a malformed tag) is
// treated as an EAN-13/ISBN-13 and re-validated (400 INVALID_ISBN on failure).
func NormalizeScanCode(raw string) (code, codeType string, err error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	if copyTagRe.MatchString(s) {
		return s, model.ScanCodeCopyBarcode, nil
	}
	isbn, err := ValidateISBN(s)
	if err != nil {
		return "", "", err
	}
	return isbn, model.ScanCodeISBN13, nil
}

// Scan resolves a scanned ISBN-13 or copy tag. Valid codes without a local row answer
// in_catalog:false / match:none rather than an error.
func (s *ScanService) Scan(ctx context.Context, raw string) (model.ScanResult, error) {
	code, codeType, err := NormalizeScanCode(raw)
	if err != nil {
		return model.ScanResult{}, err
	}
	res := model.ScanResult{Code: code, CodeType: codeType, Match: model.ScanMatchNone}

	var bookID, copyID uuid.UUID
	var found bool
	if codeType == model.ScanCodeISBN13 {
		bookID, found, err = s.repo.BookIDByISBN(ctx, code)
	} else {
		copyID, bookID, found, err = s.repo.CopyRefByBarcode(ctx, code)
	}
	if err != nil {
		return model.ScanResult{}, err
	}
	if !found {
		return res, nil
	}

	data, err := s.repo.Detail(ctx, bookID, uuid.Nil)
	if errors.Is(err, model.ErrNotFound) {
		return res, nil // removed between lookup and detail
	}
	if err != nil {
		return model.ScanResult{}, err
	}
	d := assembleDetail(data)

	if codeType == model.ScanCodeCopyBarcode {
		var cp *model.CopyDetail
		for i := range d.Copies {
			if d.Copies[i].ID == copyID {
				cp = &d.Copies[i]
				break
			}
		}
		if cp == nil {
			return res, nil
		}
		res.Copy = &model.ScanCopy{ID: cp.ID, Barcode: cp.Barcode, Status: cp.Status, Condition: cp.Condition}
		if loc, ok := data.Locations[cp.ID]; ok {
			res.Copy.Location = LocationWayfinding(loc)
			res.Wayfinding = LocationWayfinding(loc)
		}
		res.Match = model.ScanMatchCopy
		res.Returnable = cp.Status == model.CopyCheckedOut
	} else {
		res.Match = model.ScanMatchBook
		res.Wayfinding = d.Wayfinding
	}
	res.InCatalog = true
	res.Book = &model.ScanBook{
		ID: d.ID, Title: d.Title, Authors: d.Authors, ISBN13: d.ISBN13, CoverURL: d.CoverURL, Section: d.Section,
	}
	av := d.Availability
	res.Availability = &av
	return res, nil
}
