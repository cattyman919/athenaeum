package model

import "errors"

// Code is a machine-readable error code from the catalog in docs/03 §9.
type Code string

// Error catalog codes (docs/03 §9).
const (
	CodeValidationFailed         Code = "VALIDATION_FAILED"
	CodeInvalidISBN              Code = "INVALID_ISBN"
	CodeUnauthenticated          Code = "UNAUTHENTICATED"
	CodeReusedToken              Code = "REUSED_TOKEN"
	CodeInvalidCredentials       Code = "INVALID_CREDENTIALS"
	CodeForbidden                Code = "FORBIDDEN"
	CodeNotFound                 Code = "NOT_FOUND"
	CodeConflict                 Code = "CONFLICT"
	CodeEmailTaken               Code = "EMAIL_TAKEN"
	CodeISBNConflict             Code = "ISBN_CONFLICT"
	CodeBookNotAvailable         Code = "BOOK_NOT_AVAILABLE"
	CodeBorrowLimit              Code = "BORROW_LIMIT"
	CodeRateLimited              Code = "RATE_LIMITED"
	CodeUpstreamError            Code = "UPSTREAM_ERROR"
	CodeEmbeddingUnavailable     Code = "EMBEDDING_UNAVAILABLE"
	CodeTranscriptionUnavailable Code = "TRANSCRIPTION_UNAVAILABLE"
	CodeUnexpected               Code = "UNEXPECTED"
)

// Error is the typed domain error returned by services. Handlers render it
// through the single error mapper into the envelope of docs/03 §1.
type Error struct {
	Code    Code
	Message string
	// Extra holds additional envelope keys (e.g. "suggested_actions").
	Extra map[string]any
	Cause error
	// HTTPStatus overrides the code's default status when non-zero (e.g. chat
	// unconfigured → 503 with UPSTREAM_ERROR, docs/01 §7).
	HTTPStatus int
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Cause.Error()
	}
	return string(e.Code) + ": " + e.Message
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error { return e.Cause }

// Is matches by code so errors.Is(err, model.ErrNotFound) works for any
// *Error carrying that code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// New builds a domain error.
func New(code Code, message string) *Error { return &Error{Code: code, Message: message} }

// Wrap builds a domain error carrying an underlying cause (never rendered to clients).
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// WithExtra returns a copy of the error with extra envelope keys.
func (e *Error) WithExtra(extra map[string]any) *Error {
	c := *e
	c.Extra = extra
	return &c
}

// Sentinels for errors.Is comparisons.
var (
	ErrValidation         = New(CodeValidationFailed, "validation failed")
	ErrUnauthenticated    = New(CodeUnauthenticated, "authentication required")
	ErrForbidden          = New(CodeForbidden, "forbidden")
	ErrNotFound           = New(CodeNotFound, "not found")
	ErrConflict           = New(CodeConflict, "conflict")
	ErrBookUnavailable    = New(CodeBookNotAvailable, "This book is not currently available in the building.")
	ErrBorrowLimit        = New(CodeBorrowLimit, "You already have 5 loans")
	ErrRateLimited        = New(CodeRateLimited, "too many requests")
	ErrUpstream           = New(CodeUpstreamError, "upstream service error")
	ErrEmbeddingUnavail   = New(CodeEmbeddingUnavailable, "embedding service unavailable")
	ErrTranscriptionUnav  = New(CodeTranscriptionUnavailable, "transcription service unavailable")
	ErrInvalidCredentials = New(CodeInvalidCredentials, "invalid email or password")
)

// AsError extracts a *Error from err's chain.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
