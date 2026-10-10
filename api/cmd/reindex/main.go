// Command reindex chunks book descriptions and refreshes pgvector embeddings
// (docs/08 §7, docs/06 §2).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/embed"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
)

const (
	exitFailure      = 1
	exitOllamaOutage = 3 // docs/08 §8
)

func main() { os.Exit(run()) }

func run() int {
	modelCheck := flag.Bool("model-check", false, "report books whose rows use another model/chunker version and exit")
	bookID := flag.String("book", "", "reindex only this book id")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reindex:", err)
		return exitFailure
	}
	log := logging.New(cfg.IsProd())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reindex: database:", err)
		return exitFailure
	}
	defer pool.Close()

	ix := index.New(repository.NewIndexStore(pool),
		embed.NewOllama(cfg.OllamaBaseURL, cfg.OllamaEmbedModel, cfg.OllamaEmbedDim), cfg.OllamaEmbedModel, log)

	if *modelCheck {
		stale, err := ix.ModelCheck(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "reindex:", err)
			return exitFailure
		}
		for _, s := range stale {
			fmt.Printf("stale: %s (%s) chunks=%d\n", s.Title, s.ID, s.Chunks)
		}
		fmt.Printf("stale_books=%d\n", len(stale))
		return 0
	}

	sum, err := ix.Run(ctx, *bookID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reindex:", err)
		var de *model.Error
		if errors.As(err, &de) && de.Code == model.CodeEmbeddingUnavailable {
			return exitOllamaOutage
		}
		return exitFailure
	}
	fmt.Printf("indexed_books=%d chunks_written=%d stale_chunks=%d\n", sum.IndexedBooks, sum.ChunksWritten, sum.StaleChunks)
	return 0
}
