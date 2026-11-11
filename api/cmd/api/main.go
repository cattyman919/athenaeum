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
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
	"github.com/cattyman919/athenaeum/api/internal/service/covers"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
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
	catalogRepo := repository.NewCatalogRepository(pool)
	indexStore := repository.NewIndexStore(pool)
	catalogSvc := catalog.New(catalogRepo, embedder).WithPendingCounter(indexStore)
	// Barcode intake (docs/11): Open Library behind OPENLIBRARY_BASE_URL, Redis cache.
	isbnSvc := catalog.NewISBNService(catalogRepo, olbooks.New(cfg.OpenLibraryBaseURL, cfg.OfflineMode),
		repository.NewISBNCache(rdb), log)

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
			ReasoningEffort: cfg.OpenRouterReasoningEffort,
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
	// Live partials (?partial=true) use the fast model at WHISPER_PARTIAL_BASE_URL.
	transcribeSvc := ai.NewTranscribeService(ai.NewWhisper(ai.WhisperConfig{BaseURL: cfg.WhisperBaseURL}), 0, log).
		WithPartial(ai.NewWhisper(ai.WhisperConfig{BaseURL: cfg.WhisperPartialURL()}), 0)
	log.Info("voice input", "whisper_url", cfg.WhisperBaseURL, "whisper_model", cfg.WhisperModel,
		"whisper_partial_url", cfg.WhisperPartialURL(), "whisper_fast_model", cfg.WhisperFastModel)

	// Auto-index worker (docs/06 §2, docs/08 §7): embeds dirty books and backfills
	// books without embeddings; waits for Ollama with backoff instead of failing.
	indexWorker := index.NewWorker(indexStore, index.New(indexStore, embedder, cfg.OllamaEmbedModel, log), log)
	go indexWorker.Run(ctx)

	// Admin (docs/03 §8): book/copy CRUD, audit stream, building tree, users.
	adminSvc := admin.New(repository.NewAdminRepository(pool), catalogSvc, repository.NewBuildingRepository(pool)).
		WithIndexNotifier(indexWorker).WithIndexStatus(indexStore)

	srv := server.New(server.Deps{
		Config:      cfg,
		Log:         log,
		JWT:         jm,
		Auth:        authSvc,
		Catalog:     catalogSvc,
		Library:     librarySvc,
		Waitlist:    waitSvc,
		Chat:        chatSvc,
		Transcribe:  transcribeSvc,
		Admin:       adminSvc,
		ISBN:        isbnSvc,
		Scan:        catalog.NewScanService(catalogRepo),
		Circulation: circulation.New(repository.NewCirculationRepository(pool)),
		Covers: covers.New(repository.NewCoverRepository(pool),
			covers.NewHTTPFetcher(cfg.OpenLibraryCoversBaseURL, cfg.OfflineMode), log),
		Limiter: middleware.NewRateLimiter(rdb, log),
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
