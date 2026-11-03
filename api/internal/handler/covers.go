package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/covers"
)

// CoversHandler serves GET /api/books/{id}/cover (docs/03 §5.6).
type CoversHandler struct{ covers *covers.Service }

// NewCoversHandler builds the handler.
func NewCoversHandler(s *covers.Service) *CoversHandler { return &CoversHandler{covers: s} }

// Get writes the cached cover bytes. Responses are immutable for a year: payloads link
// covers as /api/books/{id}/cover?v={sha256 prefix}, so a new image gets a new URL.
func (h *CoversHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondError(c, model.New(model.CodeNotFound, "Book not found."))
		return
	}
	cover, err := h.covers.Get(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	etag := `"` + cover.SHA256 + `"`
	hdr := c.Writer.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	hdr.Set("X-Content-Type-Options", "nosniff")
	if match := c.GetHeader("If-None-Match"); match != "" && etagMatches(match, etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, cover.ContentType, cover.Bytes)
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if p == etag || p == "*" {
			return true
		}
	}
	return false
}
