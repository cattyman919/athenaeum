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

	uid, _ := middleware.UserID(c)
	isAuthed := uid.String() != "00000000-0000-0000-0000-000000000000"

	if h.circulation != nil {
		if res.Match == model.ScanMatchCopy && res.Copy != nil {
			// Enrich copy-tag matches with loan info and borrowable flag (docs/03 §5.4).
			if loan, lerr := h.circulation.GetOpenLoan(c.Request.Context(), res.Copy.ID, uid); lerr == nil {
				res.Loan = loan
			}
			if res.Copy.Status == model.CopyAvailable && isAuthed {
				if count, cerr := h.circulation.CountOpenLoans(c.Request.Context(), uid); cerr == nil {
					res.Borrowable = count < model.MaxOpenLoans
				}
			}
		}
		if res.Match == model.ScanMatchBook && res.Book != nil {
			// Enrich book-ISBN matches with borrow/return fields (plan M21, docs/03 §5.4).
			borrowable, borrowCopy, returnable, myLoans, berr := h.circulation.GetBookBorrowInfo(
				c.Request.Context(), res.Book.ID, uid)
			if berr == nil {
				res.Borrowable = borrowable
				res.BorrowCopy = borrowCopy
				res.Returnable = returnable
				if myLoans == nil {
					res.MyLoans = []model.ScanMyLoan{}
				} else {
					res.MyLoans = myLoans
				}
			}
		}
	}

	c.JSON(http.StatusOK, res)
}

// BorrowBook handles POST /api/books/{id}/borrow (plan M21, docs/03 §6.8).
func (h *ScanHandler) BorrowBook(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.BorrowBook(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

// ReturnBook handles POST /api/books/{id}/return (plan M21, docs/03 §6.9).
func (h *ScanHandler) ReturnBook(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	res, err := h.circulation.ReturnBook(c.Request.Context(), uid, c.Param("id"))
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
