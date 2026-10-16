// Package model — loan-related types (docs/03 §6.5–6.7, docs/02 §loans).
package model

import (
	"time"

	"github.com/google/uuid"
)

// LoanPeriod is the fixed checkout duration (docs/03 §6.5).
const LoanPeriod = 14 * 24 * time.Hour

// MaxOpenLoans is the per-user borrow limit (docs/03 §6.5).
const MaxOpenLoans = 5

// MaxRenewals is the number of allowed renewals per loan (docs/03 §6.6).
const MaxRenewals = 1

// ScanLoanRef is the compact loan shown on POST /api/books/scan copy-tag matches
// (docs/03 §5.4). "mine" is true when the caller is the borrower.
type ScanLoanRef struct {
	ID       uuid.UUID `json:"id"`
	UserID   uuid.UUID `json:"user_id"`
	DueAt    time.Time `json:"due_at"`
	Renewals int       `json:"renewals"`
	Mine     bool      `json:"mine"`
}

// LoanDetail is the full loan row returned in borrow and renew responses
// (docs/03 §6.5).
type LoanDetail struct {
	ID         uuid.UUID  `json:"id"`
	CopyID     uuid.UUID  `json:"copy_id"`
	UserID     uuid.UUID  `json:"user_id"`
	BorrowedAt time.Time  `json:"borrowed_at"`
	DueAt      time.Time  `json:"due_at"`
	ReturnedAt *time.Time `json:"returned_at"`
	Renewals   int        `json:"renewals"`
	Renewable  bool       `json:"renewable"`
}

// ClosedLoanRef is the minimal closed-loan record on a return response
// (docs/03 §6.4).
type ClosedLoanRef struct {
	ID         uuid.UUID `json:"id"`
	ReturnedAt time.Time `json:"returned_at"`
}

// BorrowResult is the 201 body of POST /api/copies/{id}/borrow (docs/03 §6.5).
type BorrowResult struct {
	Loan         LoanDetail         `json:"loan"`
	Copy         ReturnedCopy       `json:"copy"`
	Availability DetailAvailability `json:"availability"`
}

// RenewedLoan is the loan snapshot inside a renew response (docs/03 §6.6).
type RenewedLoan struct {
	ID        uuid.UUID `json:"id"`
	DueAt     time.Time `json:"due_at"`
	Renewals  int       `json:"renewals"`
	Renewable bool      `json:"renewable"`
}

// RenewResult is the 200 body of POST /api/copies/{id}/renew (docs/03 §6.6).
type RenewResult struct {
	Loan RenewedLoan `json:"loan"`
}

// LoanBookRef is the compact book on a loan list item.
type LoanBookRef struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	CoverURL *string   `json:"cover_url"`
	Authors  []string  `json:"authors"`
}

// LoanItem is one row in GET /api/loans (docs/03 §6.7).
// The flat copy_id/barcode shape matches web/lib/types.ts Loan.
type LoanItem struct {
	Kind       string      `json:"kind"` // "active" | "past"
	ID         uuid.UUID   `json:"id"`
	Book       LoanBookRef `json:"book"`
	CopyID     uuid.UUID   `json:"copy_id"`
	Barcode    string      `json:"barcode"`
	BorrowedAt time.Time   `json:"borrowed_at"`
	DueAt      time.Time   `json:"due_at"`
	ReturnedAt *time.Time  `json:"returned_at"`
	Renewals   int         `json:"renewals"`
	Renewable  bool        `json:"renewable"`
	Overdue    bool        `json:"overdue"`
}

// LoansMeta is the meta for GET /api/loans (docs/03 §6.7).
type LoansMeta struct {
	Active  int `json:"active"`
	Overdue int `json:"overdue"`
}

// LoansListResult is the full response for GET /api/loans.
type LoansListResult struct {
	Items []LoanItem `json:"items"`
	Meta  LoansMeta  `json:"meta"`
}

// BorrowData is the raw repository result of a borrow operation.
type BorrowData struct {
	Loan      LoanDetail
	Copy      AdminCopy
	Location  *LocationRow
	Total     int
	Available int
	Statuses  []string
}

// RenewData is the raw repository result of a renew operation.
type RenewData struct {
	ID       uuid.UUID
	DueAt    time.Time
	Renewals int
}
