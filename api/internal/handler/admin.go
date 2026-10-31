package handler

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
)

// AdminHandler serves /api/admin/* (docs/03 §8); routes are guarded by RequireAdmin.
type AdminHandler struct{ svc *admin.Service }

// NewAdminHandler builds the handler.
func NewAdminHandler(svc *admin.Service) *AdminHandler { return &AdminHandler{svc: svc} }

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// optString decodes a string-or-null JSON member into an Optional.
func optString(raw json.RawMessage) (model.Optional[string], error) {
	if isNull(raw) {
		return model.Optional[string]{Set: true, Null: true}, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return model.Optional[string]{}, badBody()
	}
	return model.Optional[string]{Set: true, Value: v}, nil
}

// sectionCode accepts "SF" or {"code":"SF"} (the detail schema) or null.
func sectionCode(raw json.RawMessage) (model.Optional[string], error) {
	if o, err := optString(raw); err == nil {
		return o, nil
	}
	var obj struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return model.Optional[string]{}, badBody()
	}
	return model.Optional[string]{Set: true, Value: obj.Code}, nil
}

func respondJSON(c *gin.Context, status int, v any, err error) {
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(status, v)
}

func noContent(c *gin.Context, err error) {
	if err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func actor(c *gin.Context) uuid.UUID {
	id, _ := middleware.UserID(c)
	return id
}

func pageQuery(c *gin.Context) (int, int, error) {
	page, err := intParam(c, "page")
	if err != nil {
		return 0, 0, err
	}
	perPage, err := intParam(c, "per_page")
	return page, perPage, err
}

// Data handles GET /api/admin/data.
func (h *AdminHandler) Data(c *gin.Context) {
	k, err := h.svc.Data(c.Request.Context())
	respondJSON(c, http.StatusOK, k, err)
}

// IndexStatus handles GET /api/admin/index-status.
func (h *AdminHandler) IndexStatus(c *gin.Context) {
	st, err := h.svc.IndexStatus(c.Request.Context())
	respondJSON(c, http.StatusOK, st, err)
}

// CopyEvents handles GET /api/admin/copy-events?since=&limit=.
func (h *AdminHandler) CopyEvents(c *gin.Context) {
	limit, err := intParam(c, "limit")
	if err != nil {
		RespondError(c, err)
		return
	}
	items, err := h.svc.CopyEvents(c.Request.Context(), c.Query("since"), limit)
	respondJSON(c, http.StatusOK, gin.H{"items": items}, err)
}

// ListBooks handles GET /api/admin/books.
func (h *AdminHandler) ListBooks(c *gin.Context) {
	page, perPage, err := pageQuery(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	res, err := h.svc.ListBooks(c.Request.Context(), admin.BookListRequest{
		Search: c.Query("search"), Shelf: c.Query("shelf"), Sort: c.Query("sort"), Page: page, PerPage: perPage,
	})
	respondJSON(c, http.StatusOK, res, err)
}

type copyBody struct {
	Barcode   string `json:"barcode"`
	Status    string `json:"status"`
	Condition string `json:"condition"`
	ShelfPath string `json:"shelf_path"`
}

func (b copyBody) input() admin.CopyInput {
	return admin.CopyInput{Barcode: b.Barcode, Status: b.Status, Condition: b.Condition, ShelfPath: b.ShelfPath}
}

// CreateBook handles POST /api/admin/books.
func (h *AdminHandler) CreateBook(c *gin.Context) {
	var body struct {
		Title          string          `json:"title"`
		Subtitle       *string         `json:"subtitle"`
		Authors        []string        `json:"authors"`
		ISBN13         *string         `json:"isbn13"`
		ISBN10         *string         `json:"isbn10"`
		Publisher      *string         `json:"publisher"`
		PublishedYear  *int            `json:"published_year"`
		Description    *string         `json:"description"`
		CoverURL       *string         `json:"cover_url"`
		OpenLibraryKey *string         `json:"open_library_key"`
		DeweyNum       *string         `json:"dewey_num"`
		LOCNum         *string         `json:"loc_num"`
		Section        json.RawMessage `json:"section"`
		Copies         []copyBody      `json:"copies"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	req := admin.CreateBookRequest{Fields: model.BookFields{
		Title: body.Title, Subtitle: body.Subtitle, Authors: body.Authors, ISBN13: body.ISBN13, ISBN10: body.ISBN10,
		Publisher: body.Publisher, PublishedYear: body.PublishedYear, Description: body.Description,
		CoverURL: body.CoverURL, OpenLibraryKey: body.OpenLibraryKey, DeweyNum: body.DeweyNum, LOCNum: body.LOCNum,
	}}
	if len(body.Section) > 0 {
		sec, err := sectionCode(body.Section)
		if err != nil {
			RespondError(c, err)
			return
		}
		req.Section = sec.Value
	}
	for _, cp := range body.Copies {
		req.Copies = append(req.Copies, cp.input())
	}
	b, err := h.svc.CreateBook(c.Request.Context(), actor(c), req)
	respondJSON(c, http.StatusCreated, b, err)
}

// PatchBook handles PATCH /api/admin/books/{id}.
func (h *AdminHandler) PatchBook(c *gin.Context) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	var req admin.BookPatchRequest
	p := &req.Patch
	if raw, found := body["title"]; found {
		var t string
		if err := json.Unmarshal(raw, &t); err != nil {
			RespondError(c, badBody())
			return
		}
		p.Title = &t
	}
	if raw, found := body["authors"]; found {
		var a []string
		if err := json.Unmarshal(raw, &a); err != nil {
			RespondError(c, badBody())
			return
		}
		p.Authors = &a
	}
	strFields := map[string]*model.Optional[string]{
		"subtitle": &p.Subtitle, "isbn13": &p.ISBN13, "isbn10": &p.ISBN10, "publisher": &p.Publisher,
		"description": &p.Description, "cover_url": &p.CoverURL, "open_library_key": &p.OpenLibraryKey,
		"dewey_num": &p.DeweyNum, "loc_num": &p.LOCNum,
	}
	for key, dst := range strFields {
		if raw, found := body[key]; found {
			o, err := optString(raw)
			if err != nil {
				RespondError(c, err)
				return
			}
			*dst = o
		}
	}
	var err error
	if raw, found := body["published_year"]; found {
		if p.PublishedYear, err = optInt(raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	if raw, found := body["section"]; found {
		if req.Section, err = sectionCode(raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	b, err := h.svc.PatchBook(c.Request.Context(), c.Param("id"), req)
	respondJSON(c, http.StatusOK, b, err)
}

// DeleteBook handles DELETE /api/admin/books/{id}.
func (h *AdminHandler) DeleteBook(c *gin.Context) {
	noContent(c, h.svc.DeleteBook(c.Request.Context(), c.Param("id")))
}

func (h *AdminHandler) BulkDeleteBooks(c *gin.Context) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	noContent(c, h.svc.DeleteBooks(c.Request.Context(), body.IDs))
}

// AddCopy handles POST /api/admin/copies.
func (h *AdminHandler) AddCopy(c *gin.Context) {
	var body struct {
		BookID string `json:"book_id"`
		copyBody
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.BookID == "" {
		RespondError(c, badBody())
		return
	}
	cp, err := h.svc.AddCopy(c.Request.Context(), actor(c), admin.AddCopyRequest{BookID: body.BookID, Copy: body.input()})
	respondJSON(c, http.StatusCreated, cp, err)
}

// PatchCopy handles PATCH /api/admin/copies/{id} ({id} = copy id or barcode).
func (h *AdminHandler) PatchCopy(c *gin.Context) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	if _, found := body["barcode"]; found {
		RespondError(c, model.New(model.CodeValidationFailed, "Barcode is immutable once registered."))
		return
	}
	var p model.CopyPatch
	for key, dst := range map[string]**string{"status": &p.Status, "condition": &p.Condition} {
		if raw, found := body[key]; found {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				RespondError(c, badBody())
				return
			}
			*dst = &s
		}
	}
	if raw, found := body["shelf_id"]; found {
		if isNull(raw) {
			p.ShelfID = model.Optional[uuid.UUID]{Set: true, Null: true}
		} else {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				RespondError(c, badBody())
				return
			}
			id, err := uuid.Parse(s)
			if err != nil {
				RespondError(c, model.New(model.CodeValidationFailed, "shelf_id must be a UUID or null."))
				return
			}
			p.ShelfID = model.Optional[uuid.UUID]{Set: true, Value: id}
		}
	}
	cp, err := h.svc.PatchCopy(c.Request.Context(), actor(c), c.Param("id"), p)
	respondJSON(c, http.StatusOK, cp, err)
}

// DeleteCopy handles DELETE /api/admin/copies/{id}.
func (h *AdminHandler) DeleteCopy(c *gin.Context) {
	noContent(c, h.svc.DeleteCopy(c.Request.Context(), c.Param("id")))
}

// Tree handles GET /api/admin/tree.
func (h *AdminHandler) Tree(c *gin.Context) {
	t, err := h.svc.Tree(c.Request.Context())
	respondJSON(c, http.StatusOK, t, err)
}

// Shelf handles GET /api/admin/shelves/{id}.
func (h *AdminHandler) Shelf(c *gin.Context) {
	s, err := h.svc.Shelf(c.Request.Context(), c.Param("id"))
	respondJSON(c, http.StatusOK, s, err)
}

// Users handles GET /api/admin/users.
func (h *AdminHandler) Users(c *gin.Context) {
	page, perPage, err := pageQuery(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	res, err := h.svc.Users(c.Request.Context(), page, perPage)
	respondJSON(c, http.StatusOK, res, err)
}

// PatchUser handles PATCH /api/admin/users/{id} body {"role":"admin"|"user"}.
func (h *AdminHandler) PatchUser(c *gin.Context) {
	var body struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, badBody())
		return
	}
	u, err := h.svc.SetRole(c.Request.Context(), actor(c), c.Param("id"), body.Role)
	respondJSON(c, http.StatusOK, u, err)
}

// AttachISBN handles POST /api/admin/books/{id}/isbns (plan M21).
func (h *AdminHandler) AttachISBN(c *gin.Context) {
	var body struct {
		ISBN13 string `json:"isbn13"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ISBN13 == "" {
		RespondError(c, badBody())
		return
	}
	result, err := h.svc.AttachISBN(c.Request.Context(), c.Param("id"), body.ISBN13)
	respondJSON(c, http.StatusCreated, result, err)
}

// DetachISBN handles DELETE /api/admin/books/{id}/isbns/{isbn13} (plan M21).
func (h *AdminHandler) DetachISBN(c *gin.Context) {
	noContent(c, h.svc.DetachISBN(c.Request.Context(), c.Param("id"), c.Param("isbn13")))
}

// MergeBooks handles POST /api/admin/books/{id}/merge (plan M21).
func (h *AdminHandler) MergeBooks(c *gin.Context) {
	var body struct {
		Into string `json:"into"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Into == "" {
		RespondError(c, badBody())
		return
	}
	result, err := h.svc.MergeBooks(c.Request.Context(), actor(c), c.Param("id"), body.Into)
	respondJSON(c, http.StatusOK, result, err)
}
