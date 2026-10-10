// Command api runs the Athenaeum HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/embed"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/oauth"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/waitlist"
)

const (
	shutdownTimeout       = 10 * time.Second
	waitlistSweepInterval = 5 * time.Minute
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.IsProd())
	slog.SetDefault(log)

	jm, err := jwt.NewManager(cfg.JWTAccessSecret, cfg.AccessTTL)
	if err != nil {
		return fmt.Errorf("JWT_ACCESS_SECRET is required (copy .env.example to .env): %w", err)
	}
	if cfg.JWTRefreshSecret == "" {
		return errors.New("JWT_REFRESH_SECRET is required (copy .env.example to .env)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
	defer func() { _ = rdb.Close() }()

	var google auth.Google
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		google = oauth.NewGoogle(cfg.GoogleIssuer, cfg.GoogleClientID, cfg.GoogleClientSecret,
			cfg.WebOrigin+"/api/auth/google/callback")
	}
	authSvc := auth.New(repository.NewUserRepository(pool), repository.NewTokenRepository(pool),
		jm, google, cfg.RefreshTTL, log)

	embedder := embed.NewOllama(cfg.OllamaBaseURL, cfg.OllamaEmbedModel, cfg.OllamaEmbedDim)
	catalogSvc := catalog.New(repository.NewCatalogRepository(pool), embedder)

	libRepo := repository.NewLibraryRepository(pool)
	waitRepo := repository.NewWaitlistRepository(pool)
	waitSvc := waitlist.New(waitRepo, libRepo)
	librarySvc := library.New(libRepo, waitRepo)
	go waitSvc.RunSweeper(ctx, waitlistSweepInterval, log)

	// Chat (docs/06): without OPENROUTER_API_KEY the engine stays nil → POST /api/chat 503.
	var engine ai.Engine
	if cfg.OpenRouterAPIKey != "" {
		chatModel := ai.NewOpenRouter(ai.OpenRouterConfig{
			BaseURL: cfg.OpenRouterBaseURL, APIKey: cfg.OpenRouterAPIKey, Model: cfg.OpenRouterModel,
		})
		eng, err := ai.NewEinoEngine(ctx, chatModel, catalogSvc)
		if err != nil {
			return fmt.Errorf("chat agent: %w", err)
		}
		engine = eng
	} else {
		log.Warn("OPENROUTER_API_KEY not set; /api/chat will return 503")
	}
	chatSvc := ai.New(repository.NewChatRepository(pool), embedder, engine,
		ai.Config{Enabled: engine != nil, Model: cfg.OpenRouterModel}, log)

	// Voice input (docs/15): Whisper down → POST /api/chat/transcribe 503.
	transcribeSvc := ai.NewTranscribeService(ai.NewWhisper(ai.WhisperConfig{BaseURL: cfg.WhisperBaseURL}), 0, log)
	log.Info("voice input", "whisper_url", cfg.WhisperBaseURL, "whisper_model", cfg.WhisperModel)

	srv := server.New(server.Deps{
		Config:     cfg,
		Log:        log,
		JWT:        jm,
		Auth:       authSvc,
		Catalog:    catalogSvc,
		Library:    librarySvc,
		Waitlist:   waitSvc,
		Chat:       chatSvc,
		Transcribe: transcribeSvc,
		Limiter:    middleware.NewRateLimiter(rdb, log),
	}, fmt.Sprintf(":%d", cfg.APIPort))

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.AppEnv)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server failed: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down", "timeout", shutdownTimeout)
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
	}
	return nil
}
