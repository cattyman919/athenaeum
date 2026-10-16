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

// ScanHandler serves the patron scanner endpoints (docs/11 §9) and the
// self-service circulation endpoints (docs/03 §6.4–6.7).
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

	// Enrich copy-tag matches with loan info and borrowable flag (docs/03 §5.4).
	if res.Match == model.ScanMatchCopy && res.Copy != nil && h.circulation != nil {
		uid, _ := middleware.UserID(c)
		if loan, lerr := h.circulation.GetOpenLoan(c.Request.Context(), res.Copy.ID, uid); lerr == nil {
			res.Loan = loan
		}
		// borrowable: copy is available AND caller is under the borrow limit.
		if res.Copy.Status == model.CopyAvailable && uid.String() != "00000000-0000-0000-0000-000000000000" {
			if count, cerr := h.circulation.CountOpenLoans(c.Request.Context(), uid); cerr == nil {
				res.Borrowable = count < model.MaxOpenLoans
			}
		}
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

// Borrow handles POST /api/copies/{id}/borrow (docs/03 §6.5). The body is empty.
func (h *ScanHandler) Borrow(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.Borrow(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

// Renew handles POST /api/copies/{id}/renew (docs/03 §6.6). The body is empty.
func (h *ScanHandler) Renew(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.Renew(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListLoans handles GET /api/loans (docs/03 §6.7).
func (h *ScanHandler) ListLoans(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.ListLoans(c.Request.Context(), uid)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
