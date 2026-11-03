// Command seed loads the mock building tree, books and demo users (docs/08).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/covers"
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
	offline := flag.Bool("offline", false, "skip Open Library enrichment and cover downloads")
	coversOnly := flag.Bool("covers-only", false, "only download uncached covers into book_covers (docs/08 §6a)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.IsProd())

	noNetwork := cfg.OfflineMode || *offline

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

	coverSvc := covers.New(repository.NewCoverRepository(pool),
		covers.NewHTTPFetcher(cfg.OpenLibraryCoversBaseURL, noNetwork), log)
	if *coversOnly {
		return warmCovers(ctx, coverSvc, noNetwork)
	}

	counts, err := seed.Run(ctx, repository.NewSeedStore(pool),
		olbooks.New(cfg.OpenLibraryBaseURL, noNetwork), fx,
		seed.Options{Reset: *reset, Users: []seed.UserSpec{
			{Email: cfg.SeedAdminEmail, Name: "Library Admin", Password: cfg.SeedAdminPassword, Role: model.RoleAdmin},
			{Email: cfg.SeedUserEmail, Name: "Demo Patron", Password: cfg.SeedUserPassword, Role: model.RoleUser},
		}}, log)
	if err != nil {
		return err
	}
	fmt.Printf("seeded: users=%d books=%d copies=%d shelves=%d (checked_out=%d zero_copy_books=%d)\n",
		counts.Users, counts.Books, counts.Copies, counts.Shelves, counts.CheckedOut, counts.ZeroCopyBooks)
	if err := warmCovers(ctx, coverSvc, noNetwork); err != nil {
		return err
	}
	fmt.Println("next: cd api && go run ./cmd/reindex   # embeddings (docs/06 §2)")
	return nil
}

// warmCovers downloads every uncached cover into Postgres so the demo never waits on
// Open Library (docs/08 §6a). Individual failures are logged and left for the API's
// read-through; only database errors fail the command.
func warmCovers(ctx context.Context, svc *covers.Service, offline bool) error {
	if offline {
		fmt.Println("covers: skipped (offline mode); the API fetches them on first view when online")
		return nil
	}
	start := time.Now()
	res, err := svc.Warm(ctx, covers.DefaultWorkers)
	if err != nil {
		return fmt.Errorf("covers: %w", err)
	}
	fmt.Printf("covers: fetched=%d missing=%d failed=%d pruned=%d (%s)\n",
		res.Fetched, res.Missing, res.Failed, res.Pruned, time.Since(start).Round(time.Millisecond))
	return nil
}
