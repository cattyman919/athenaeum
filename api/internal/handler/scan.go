package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
)

// ScanHandler serves the patron scanner endpoints (docs/11 §9): POST /api/books/scan and
// POST /api/copies/{id}/return.
type ScanHandler struct {
	scan        *catalog.ScanService
	circulation *circulation.Service
}

// NewScanHandler builds the handler; either service may be nil when its route is not mounted.
func NewScanHandler(s *catalog.ScanService, c *circulation.Service) *ScanHandler {
	return &ScanHandler{scan: s, circulation: c}
}

type scanRequest struct {
	Code string `json:"code"`
}

// Scan handles POST /api/books/scan (docs/03 §5.4).
func (h *ScanHandler) Scan(c *gin.Context) {
	var req scanRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		RespondError(c, model.New(model.CodeValidationFailed, "code is required."))
		return
	}
	res, err := h.scan.Scan(c.Request.Context(), req.Code)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Return handles POST /api/copies/{id}/return (docs/03 §6.4). The body is empty.
func (h *ScanHandler) Return(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.Return(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
