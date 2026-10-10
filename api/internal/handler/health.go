package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health serves GET /api/health (docs/03 §4.1).
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "version": "v1"})
}
