package index

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Worker defaults (docs/06 §2): 5 s poll fallback, exponential retry capped at 30 s.
const (
	DefaultPollInterval = 5 * time.Second
	DefaultBackoffBase  = time.Second
	DefaultBackoffMax   = 30 * time.Second
)

// WorkerStore is the backlog persistence the auto-index worker needs (repository.IndexStore).
type WorkerStore interface {
	// PendingBooks lists dirty books plus non-blank books without embedding rows.
	PendingBooks(ctx context.Context) ([]model.PendingBook, error)
	// ClearDirty resets the flag only if the book is unchanged since snapshot.
	ClearDirty(ctx context.Context, bookID string, snapshot time.Time) (bool, error)
}

// Runner re-embeds one book (*Indexer).
type Runner interface {
	Run(ctx context.Context, bookID string) (Summary, error)
}

// Worker is the in-process auto-index loop (docs/06 §2, docs/08 §7): it embeds
// admin-created/edited books and backfills books with no embeddings, waiting for
// Ollama with backoff instead of failing.
type Worker struct {
	store       WorkerStore
	runner      Runner
	log         *slog.Logger
	notify      chan struct{}
	poll        time.Duration
	backoffBase time.Duration
	backoffMax  time.Duration
}

// NewWorker builds a worker with the default timings.
func NewWorker(store WorkerStore, runner Runner, log *slog.Logger) *Worker {
	return &Worker{
		store: store, runner: runner, log: log, notify: make(chan struct{}, 1),
		poll: DefaultPollInterval, backoffBase: DefaultBackoffBase, backoffMax: DefaultBackoffMax,
	}
}

// WithTimings overrides the poll interval and backoff bounds (tests).
func (w *Worker) WithTimings(poll, backoffBase, backoffMax time.Duration) *Worker {
	w.poll, w.backoffBase, w.backoffMax = poll, backoffBase, backoffMax
	return w
}

// Notify wakes the worker without blocking; extra signals coalesce.
func (w *Worker) Notify() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// Run processes the backlog until ctx is cancelled. It never returns an error:
// failures are logged and retried with exponential backoff.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("auto-index worker started", "poll", w.poll)
	var backoff time.Duration
	for ctx.Err() == nil {
		var wait time.Duration
		if w.drain(ctx) {
			backoff = 0
			wait = w.poll
		} else {
			backoff = w.nextBackoff(backoff)
			wait = backoff
		}
		if !w.sleep(ctx, wait, backoff == 0) {
			break
		}
	}
	w.log.Info("auto-index worker stopped")
}

func (w *Worker) nextBackoff(cur time.Duration) time.Duration {
	if cur <= 0 {
		return w.backoffBase
	}
	return min(cur*2, w.backoffMax)
}

// sleep waits for d (or a notify when wakeable); false once ctx is done.
func (w *Worker) sleep(ctx context.Context, d time.Duration, wakeable bool) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	var wake <-chan struct{}
	if wakeable {
		wake = w.notify
	}
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-t.C:
		return true
	}
}

// drain embeds every pending book once, one Indexer.Run per book. It reports
// false when any book failed so the caller backs off; an embedding outage
// stops the pass early since every remaining book would fail the same way.
func (w *Worker) drain(ctx context.Context) bool {
	pending, err := w.store.PendingBooks(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.log.Error("auto-index pending lookup failed", "err", err)
		}
		return false
	}
	if len(pending) == 0 {
		return true
	}
	w.log.Info("auto-index pending", "pending", len(pending))
	ok := true
	for i, p := range pending {
		if ctx.Err() != nil {
			return true
		}
		w.log.Info("auto-index start", "book_id", p.ID, "dirty", p.Dirty)
		sum, err := w.runner.Run(ctx, p.ID)
		if err != nil {
			if ctx.Err() != nil {
				return true
			}
			ok = false
			w.log.Warn("auto-index failed; will retry", "book_id", p.ID, "err", err,
				"pending", len(pending)-i)
			var de *model.Error
			if errors.As(err, &de) && de.Code == model.CodeEmbeddingUnavailable {
				return false
			}
			continue
		}
		if p.Dirty {
			cleared, err := w.store.ClearDirty(ctx, p.ID, p.UpdatedAt)
			if err != nil {
				if ctx.Err() != nil {
					return true
				}
				ok = false
				w.log.Warn("auto-index clear failed; will retry", "book_id", p.ID, "err", err)
				continue
			}
			if !cleared {
				w.log.Info("auto-index book changed during embed; requeued", "book_id", p.ID)
			}
		}
		w.log.Info("auto-index success", "book_id", p.ID, "chunks", sum.ChunksWritten,
			"pending", len(pending)-i-1)
	}
	return ok
}
