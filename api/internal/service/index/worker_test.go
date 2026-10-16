package index_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
)

type wBook struct {
	model.IndexBook
	dirty   bool
	updated time.Time
	rows    int
	runs    int
}

// workerStore implements index.Store and index.WorkerStore in memory, mirroring
// repository.IndexStore's pending rule.
type workerStore struct {
	mu        sync.Mutex
	books     map[string]*wBook
	order     []string
	onReplace func(id string)
}

func newWorkerStore(bs ...*wBook) *workerStore {
	s := &workerStore{books: map[string]*wBook{}}
	for _, b := range bs {
		if b.updated.IsZero() {
			b.updated = time.Unix(1, 0)
		}
		s.books[b.ID] = b
		s.order = append(s.order, b.ID)
	}
	return s
}

func (s *workerStore) get(id string) wBook {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.books[id]
}

func (s *workerStore) Books(_ context.Context, id string) ([]model.IndexBook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.books[id]; ok {
		b.runs++
		return []model.IndexBook{b.IndexBook}, nil
	}
	return nil, nil
}

func (s *workerStore) Replace(_ context.Context, ids []string, rows []model.ChunkRow) error {
	s.mu.Lock()
	for _, id := range ids {
		s.books[id].rows = 0
	}
	for _, r := range rows {
		s.books[r.BookID].rows++
	}
	hook := s.onReplace
	s.mu.Unlock()
	if hook != nil {
		for _, id := range ids {
			hook(id)
		}
	}
	return nil
}

func (s *workerStore) StaleBooks(context.Context, string, string) ([]model.StaleBook, error) {
	return nil, nil
}

func (s *workerStore) StaleChunkCount(context.Context, string, string) (int, error) { return 0, nil }

func (s *workerStore) PendingBooks(context.Context) ([]model.PendingBook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.PendingBook
	for _, id := range s.order {
		b := s.books[id]
		if b.dirty || (len(index.Chunk(b.Description)) > 0 && b.rows == 0) {
			out = append(out, model.PendingBook{ID: id, Dirty: b.dirty, UpdatedAt: b.updated})
		}
	}
	return out, nil
}

func (s *workerStore) ClearDirty(_ context.Context, id string, snap time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.books[id]
	if !b.dirty || !b.updated.Equal(snap) {
		return false, nil
	}
	b.dirty = false
	return true, nil
}

// flakyEmb fails with EMBEDDING_UNAVAILABLE while down is set.
type flakyEmb struct {
	down  atomic.Bool
	calls atomic.Int64
}

func (e *flakyEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	e.calls.Add(1)
	if e.down.Load() {
		return nil, model.New(model.CodeEmbeddingUnavailable, "ollama down")
	}
	return make([][]float32, len(texts)), nil
}

func startWorker(t *testing.T, st *workerStore, emb index.Embedder, poll time.Duration) *index.Worker {
	t.Helper()
	w := index.NewWorker(st, index.New(st, emb, "m", logging.Discard()), logging.Discard()).
		WithTimings(poll, time.Millisecond, 8*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("worker did not stop on ctx cancel")
		}
	})
	return w
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func book(id, desc string, dirty bool) *wBook {
	return &wBook{IndexBook: model.IndexBook{ID: id, Title: "T" + id, Description: desc}, dirty: dirty}
}

func TestWorkerSuccessClearsDirty(t *testing.T) {
	st := newWorkerStore(book("a", "A sentence.", true))
	startWorker(t, st, &flakyEmb{}, 10*time.Millisecond)
	eventually(t, "dirty cleared", func() bool { b := st.get("a"); return !b.dirty && b.rows == 1 })
}

func TestWorkerBackfillsBooksWithoutEmbeddings(t *testing.T) {
	st := newWorkerStore(book("a", "Seeded blurb.", false), book("b", "Another one.", false))
	startWorker(t, st, &flakyEmb{}, 10*time.Millisecond)
	eventually(t, "backfill", func() bool { return st.get("a").rows == 1 && st.get("b").rows == 1 })
}

func TestWorkerEmptyDescriptionDoesNotLoop(t *testing.T) {
	st := newWorkerStore(book("e", "", true), book("w", "   ", false))
	startWorker(t, st, &flakyEmb{}, time.Millisecond)
	eventually(t, "cleared", func() bool { return !st.get("e").dirty })
	time.Sleep(50 * time.Millisecond)
	if r := st.get("e").runs; r != 1 {
		t.Fatalf("empty-description book embedded %d times, want 1", r)
	}
	if r := st.get("w").runs; r != 0 {
		t.Fatalf("blank undirty book should never be pending, ran %d", r)
	}
}

func TestWorkerFailureKeepsPendingAndRetries(t *testing.T) {
	st := newWorkerStore(book("a", "A sentence.", true))
	emb := &flakyEmb{}
	emb.down.Store(true)
	startWorker(t, st, emb, time.Hour)
	eventually(t, "retries", func() bool { return emb.calls.Load() >= 3 })
	if b := st.get("a"); !b.dirty || b.rows != 0 {
		t.Fatalf("failed embed must keep the book pending: %+v", b)
	}
	emb.down.Store(false)
	eventually(t, "recovered", func() bool { b := st.get("a"); return !b.dirty && b.rows == 1 })
}

func TestWorkerConcurrentEditKeepsDirty(t *testing.T) {
	st := newWorkerStore(book("a", "A sentence.", true))
	var edits atomic.Int64
	st.onReplace = func(id string) {
		if edits.Add(1) == 1 { // an admin edit lands while the first embed runs
			st.mu.Lock()
			st.books[id].updated = st.books[id].updated.Add(time.Second)
			st.mu.Unlock()
		}
	}
	startWorker(t, st, &flakyEmb{}, time.Millisecond)
	eventually(t, "re-embedded after edit", func() bool { return !st.get("a").dirty })
	if r := st.get("a").runs; r < 2 {
		t.Fatalf("edited book must be embedded again, runs=%d", r)
	}
}

func TestWorkerNotifyWakesBeforePoll(t *testing.T) {
	st := newWorkerStore()
	w := startWorker(t, st, &flakyEmb{}, time.Hour)
	time.Sleep(10 * time.Millisecond) // first pass finds nothing, then sleeps an hour
	st.mu.Lock()
	b := book("n", "New book.", true)
	b.updated = time.Unix(1, 0)
	st.books["n"], st.order = b, append(st.order, "n")
	st.mu.Unlock()
	w.Notify()
	w.Notify() // coalesces, never blocks
	eventually(t, "notified embed", func() bool { return !st.get("n").dirty })
}
