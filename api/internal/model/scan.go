package model

import (
	"time"

	"github.com/google/uuid"
)

// Scan code types and match kinds (docs/03 §5.4).
const (
	ScanCodeISBN13      = "isbn13"
	ScanCodeCopyBarcode = "copy_barcode"

	ScanMatchBook = "book"
	ScanMatchCopy = "copy"
	ScanMatchNone = "none"
)

// ScanBook is the compact book record of a scan result.
type ScanBook struct {
	ID       uuid.UUID   `json:"id"`
	Title    string      `json:"title"`
	Authors  []string    `json:"authors"`
	ISBN13   *string     `json:"isbn13"`
	CoverURL *string     `json:"cover_url"`
	Section  *SectionRef `json:"section"`
}

// ScanCopy is the scanned physical copy of a copy-tag match.
type ScanCopy struct {
	ID        uuid.UUID   `json:"id"`
	Barcode   string      `json:"barcode"`
	Status    string      `json:"status"`
	Condition string      `json:"condition"`
	Location  *Wayfinding `json:"location"`
}

// ScanResult is POST /api/books/scan (docs/03 §5.4).
type ScanResult struct {
	Code         string              `json:"code"`
	CodeType     string              `json:"code_type"`
	InCatalog    bool                `json:"in_catalog"`
	Match        string              `json:"match"`
	Book         *ScanBook           `json:"book"`
	Copy         *ScanCopy           `json:"copy"`
	Availability *DetailAvailability `json:"availability"`
	Wayfinding   *Wayfinding         `json:"wayfinding"`
	Returnable   bool                `json:"returnable"`
	Borrowable   bool                `json:"borrowable"`
	Loan         *ScanLoanRef        `json:"loan"`
}

// ReturnedCopy is the copy object of POST /api/copies/{id}/return (docs/03 §6.4).
type ReturnedCopy struct {
	ID              uuid.UUID   `json:"id"`
	Barcode         string      `json:"barcode"`
	Status          string      `json:"status"`
	Condition       string      `json:"condition"`
	LastStateChange time.Time   `json:"last_state_change"`
	Location        *Wayfinding `json:"location"`
}

// PromotedHold names the waitlist entry promoted to ready by a return.
type PromotedHold struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ReturnResult is POST /api/copies/{id}/return (docs/03 §6.4).
type ReturnResult struct {
	Copy         ReturnedCopy       `json:"copy"`
	Availability DetailAvailability `json:"availability"`
	Hold         *PromotedHold      `json:"hold"`
	ClosedLoan   *ClosedLoanRef     `json:"closed_loan"`
}

// ReturnData is the raw repository result of a self-service return; the service derives
// the location steps and availability state from it.
type ReturnData struct {
	Copy       AdminCopy
	Location   *LocationRow
	Total      int
	Available  int
	Statuses   []string
	Hold       *PromotedHold
	ClosedLoan *ClosedLoanRef
}
