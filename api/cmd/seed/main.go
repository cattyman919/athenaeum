// Command seed loads the mock building tree, books and demo users (docs/08).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
	"github.com/cattyman919/athenaeum/api/internal/service/seed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	mocks := flag.String("mocks", "./mocks", "directory with building.json and books.json")
	reset := flag.Bool("reset", false, "truncate catalog content before seeding")
	offline := flag.Bool("offline", false, "skip Open Library enrichment")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.IsProd())

	fx, err := seed.Load(*mocks)
	if err != nil {
		return err
	}
	if err := fx.Validate(); err != nil {
		return err // bad fixture: abort before any write
	}
	fx.Library.UserEmail = cfg.SeedUserEmail // the demo library follows SEED_USER_EMAIL

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}

	counts, err := seed.Run(ctx, repository.NewSeedStore(pool),
		olbooks.New(cfg.OpenLibraryBaseURL, cfg.OfflineMode || *offline), fx,
		seed.Options{Reset: *reset, Users: []seed.UserSpec{
			{Email: cfg.SeedAdminEmail, Name: "Library Admin", Password: cfg.SeedAdminPassword, Role: model.RoleAdmin},
			{Email: cfg.SeedUserEmail, Name: "Demo Patron", Password: cfg.SeedUserPassword, Role: model.RoleUser},
		}}, log)
	if err != nil {
		return err
	}
	fmt.Printf("seeded: users=%d books=%d copies=%d shelves=%d (checked_out=%d zero_copy_books=%d)\n",
		counts.Users, counts.Books, counts.Copies, counts.Shelves, counts.CheckedOut, counts.ZeroCopyBooks)
	fmt.Println("next: cd api && go run ./cmd/reindex   # embeddings (docs/06 §2)")
	return nil
}
