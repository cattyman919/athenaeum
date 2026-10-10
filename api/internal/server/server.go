// Package server assembles the Gin engine (routes + middleware chain).
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/handler"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
)

// NewRouter builds the engine with the shared middleware chain and all routes.
func NewRouter(cfg config.Config, log *slog.Logger) *gin.Engine {
	if cfg.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Recover(log),
		middleware.AccessLog(log),
		middleware.CORS(cfg.WebOrigin),
	)
	r.NoRoute(func(c *gin.Context) {
		handler.RespondError(c, model.New(model.CodeNotFound, "Route not found."))
	})

	api := r.Group("/api")
	api.GET("/health", handler.Health)
	return r
}

// New returns the http.Server for the configured port.
func New(cfg config.Config, log *slog.Logger, addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(cfg, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
