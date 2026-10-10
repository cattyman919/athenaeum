package handler

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/waitlist"
)

// LibraryHandler serves /api/library and /api/waitlist (docs/03 §6).
type LibraryHandler struct {
	library  *library.Service
	waitlist *waitlist.Service
}

// NewLibraryHandler builds the handler.
func NewLibraryHandler(l *library.Service, w *waitlist.Service) *LibraryHandler {
	return &LibraryHandler{library: l, waitlist: w}
}

func badBody() error { return model.New(model.CodeValidationFailed, "Invalid request body.") }

// List handles GET /api/library.
func (h *LibraryHandler) List(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	page, err := intParam(c, "page")
	if err != nil {
		RespondError(c, err)
		return
	}
	perPage, err := intParam(c, "per_page")
	if err != nil {
		RespondError(c, err)
		return
	}
	res, err := h.library.List(c.Request.Context(), uid, library.ListRequest{
		Statuses:        c.QueryArray("status"),
		IncludeWaitlist: c.Query("include_waitlist") == "true",
		Page:            page,
		PerPage:         perPage,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Add handles POST /api/library.
func (h *LibraryHandler) Add(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	var req struct {
		BookID string  `json:"book_id"`
		Status *string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.BookID == "" {
		RespondError(c, badBody())
		return
	}
	e, created, err := h.library.Add(c.Request.Context(), uid, req.BookID, req.Status, c.Query("force") == "true")
	if err != nil {
		RespondError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, e)
}

// optInt decodes an int-or-null JSON member into an Optional.
func optInt(raw json.RawMessage) (model.Optional[int], error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return model.Optional[int]{Set: true, Null: true}, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return model.Optional[int]{}, badBody()
	}
	return model.Optional[int]{Set: true, Value: v}, nil
}

// Patch handles PATCH /api/library/{book_id}.
func (h *LibraryHandler) Patch(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	var p model.LibraryPatch
	if raw, ok := body["status"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			RespondError(c, badBody())
			return
		}
		p.Status = &s
	}
	if raw, ok := body["is_favorite"]; ok {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			RespondError(c, badBody())
			return
		}
		p.IsFavorite = &b
	}
	var err error
	if raw, ok := body["progress_pct"]; ok {
		if p.ProgressPct, err = optInt(raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	if raw, ok := body["rating"]; ok {
		if p.Rating, err = optInt(raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	e, err := h.library.Patch(c.Request.Context(), uid, c.Param("book_id"), p)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}

// Remove handles DELETE /api/library/{book_id}.
func (h *LibraryHandler) Remove(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	if err := h.library.Remove(c.Request.Context(), uid, c.Param("book_id")); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// JoinWaitlist handles POST /api/books/{id}/waitlist.
func (h *LibraryHandler) JoinWaitlist(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	e, created, err := h.waitlist.Join(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"id": e.ID, "status": e.Status, "position": e.Position})
}

// LeaveWaitlist handles DELETE /api/books/{id}/waitlist.
func (h *LibraryHandler) LeaveWaitlist(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	if err := h.waitlist.Leave(c.Request.Context(), uid, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListWaitlist handles GET /api/waitlist.
func (h *LibraryHandler) ListWaitlist(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	items, err := h.waitlist.List(c.Request.Context(), uid)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
