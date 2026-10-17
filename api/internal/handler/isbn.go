package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// ISBNHandler serves POST /api/books/fetch-isbn (docs/03 §5.3, docs/11).
type ISBNHandler struct {
	svc *catalog.ISBNService
}

// NewISBNHandler builds the handler.
func NewISBNHandler(s *catalog.ISBNService) *ISBNHandler { return &ISBNHandler{svc: s} }

type fetchISBNRequest struct {
	ISBN string `json:"isbn"`
}

// FetchISBN handles POST /api/books/fetch-isbn.
func (h *ISBNHandler) FetchISBN(c *gin.Context) {
	var req fetchISBNRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ISBN == "" {
		RespondError(c, model.New(model.CodeValidationFailed, "isbn is required."))
		return
	}
	md, err := h.svc.FetchISBN(c.Request.Context(), req.ISBN)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, md)
}

type identifyISBNRequest struct {
	ISBN string `json:"isbn"`
}

// Identify handles POST /api/books/identify-isbn (docs/03 §5.5).
// Available to any signed-in user; never returns 5xx for upstream trouble.
func (h *ISBNHandler) Identify(c *gin.Context) {
	var req identifyISBNRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ISBN == "" {
		RespondError(c, model.New(model.CodeValidationFailed, "isbn is required."))
		return
	}
	result, err := h.svc.Identify(c.Request.Context(), req.ISBN)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
