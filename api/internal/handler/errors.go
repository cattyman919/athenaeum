// Package handler holds the HTTP layer: parsing, validation and error mapping.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

var statusByCode = map[model.Code]int{
	model.CodeValidationFailed:         http.StatusBadRequest,
	model.CodeInvalidISBN:              http.StatusBadRequest,
	model.CodeUnauthenticated:          http.StatusUnauthorized,
	model.CodeReusedToken:              http.StatusUnauthorized,
	model.CodeInvalidCredentials:       http.StatusUnauthorized,
	model.CodeForbidden:                http.StatusForbidden,
	model.CodeNotFound:                 http.StatusNotFound,
	model.CodeConflict:                 http.StatusConflict,
	model.CodeEmailTaken:               http.StatusConflict,
	model.CodeISBNConflict:             http.StatusConflict,
	model.CodeBookNotAvailable:         http.StatusConflict,
	model.CodeRateLimited:              http.StatusTooManyRequests,
	model.CodeUpstreamError:            http.StatusBadGateway,
	model.CodeEmbeddingUnavailable:     http.StatusServiceUnavailable,
	model.CodeTranscriptionUnavailable: http.StatusServiceUnavailable,
	model.CodeUnexpected:               http.StatusInternalServerError,
}

// RespondError is the single error mapper (docs/01 §3): it renders any error
// as the error envelope of docs/03 §1. Unknown errors become 500 UNEXPECTED
// without leaking details.
func RespondError(c *gin.Context, err error) {
	de, ok := model.AsError(err)
	if !ok {
		slog.ErrorContext(c.Request.Context(), "unhandled error", "err", err, "request_id", c.GetString("request_id"))
		de = model.New(model.CodeUnexpected, "An unexpected error occurred.")
	} else if de.Cause != nil {
		slog.WarnContext(c.Request.Context(), "request failed", "code", de.Code, "err", de.Cause, "request_id", c.GetString("request_id"))
	}
	status, found := statusByCode[de.Code]
	if !found {
		status = http.StatusInternalServerError
	}
	if de.HTTPStatus != 0 {
		status = de.HTTPStatus
	}
	body := gin.H{"error": string(de.Code), "message": de.Message}
	for k, v := range de.Extra {
		if k != "error" && k != "message" {
			body[k] = v
		}
	}
	c.AbortWithStatusJSON(status, body)
}
