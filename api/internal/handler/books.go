package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// BooksHandler serves /api/books/* (docs/03 §5).
type BooksHandler struct {
	catalog *catalog.Service
}

// NewBooksHandler builds the handler.
func NewBooksHandler(c *catalog.Service) *BooksHandler { return &BooksHandler{catalog: c} }

func intParam(c *gin.Context, name string) (int, error) {
	v := c.Query(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, model.New(model.CodeValidationFailed, name+" must be an integer.")
	}
	return n, nil
}

// Search handles GET /api/books/search.
func (h *BooksHandler) Search(c *gin.Context) {
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
	avail := c.DefaultQuery("availability", "any")
	if avail != "any" && avail != "available" {
		RespondError(c, model.New(model.CodeValidationFailed, "availability must be any or available."))
		return
	}
	res, err := h.catalog.Search(c.Request.Context(), catalog.Request{
		Query: c.Query("q"),
		Mode:  c.Query("mode"),
		Filters: model.SearchFilters{
			Sections:      c.QueryArray("section"),
			AvailableOnly: avail == "available",
			Eras:          c.QueryArray("era"),
		},
		Sort:    c.Query("sort"),
		Page:    page,
		PerPage: perPage,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Get handles GET /api/books/{id} (docs/03 §5.2).
func (h *BooksHandler) Get(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	d, err := h.catalog.Detail(c.Request.Context(), c.Param("id"), uid)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}
