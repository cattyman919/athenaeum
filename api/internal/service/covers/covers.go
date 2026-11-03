// Package covers caches book cover images in Postgres and serves them from the API
// (docs/02 §book_covers, docs/03 §5.6, docs/08 §6a). The upstream URL stays in
// books.cover_url; the bytes are fetched once — by the seed warm-up or read-through on
// the first request — and every later request is a single indexed row read.
package covers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

const (
	// MaxBytes caps one cover (matches the book_covers CHECK constraint).
	MaxBytes = 2 << 20
	// minBytes rejects tracking-pixel placeholders (Open Library answers a 43-byte
	// 1×1 GIF for unknown covers unless ?default=false is honoured).
	minBytes = 1024
	// fetchTimeout bounds one upstream fetch; Open Library covers redirect to
	// archive.org and regularly take several seconds.
	fetchTimeout = 10 * time.Second
	// missTTL suppresses re-fetching a cover that just failed.
	missTTL = 15 * time.Minute
	// DefaultWorkers is the warm-up concurrency (polite to Open Library).
	DefaultWorkers = 4
)

// OpenLibraryCoversBase is the host books.cover_url values point at.
const OpenLibraryCoversBase = "https://covers.openlibrary.org"

var (
	// ErrNoCover means upstream has no usable image for the URL.
	ErrNoCover = errors.New("covers: no cover image upstream")
	// ErrOffline means fetching was skipped (OFFLINE_MODE / --offline).
	ErrOffline = errors.New("covers: offline mode")
)

// Store persists covers (implemented by repository.CoverRepository).
type Store interface {
	// ForBook returns the book's upstream cover URL ("" when none) and the cached
	// image (nil on a miss); NOT_FOUND when the book does not exist.
	ForBook(ctx context.Context, bookID uuid.UUID) (string, *model.Cover, error)
	Upsert(ctx context.Context, c model.Cover) error
	// Uncached lists distinct books.cover_url values without cached bytes.
	Uncached(ctx context.Context) ([]string, error)
	// Prune deletes cached covers no book references.
	Prune(ctx context.Context) (int64, error)
}

// Fetcher downloads cover images.
type Fetcher interface {
	Fetch(ctx context.Context, sourceURL string) (model.Cover, error)
}

// HTTPFetcher fetches covers over HTTP. URLs on covers.openlibrary.org are rewritten
// onto base (OPENLIBRARY_COVERS_BASE_URL; the e2e stubs serve covers there).
type HTTPFetcher struct {
	base    string
	offline bool
	http    *http.Client
}

// NewHTTPFetcher builds a fetcher; offline=true makes every Fetch return ErrOffline.
func NewHTTPFetcher(base string, offline bool) *HTTPFetcher {
	return &HTTPFetcher{
		base: strings.TrimRight(base, "/"), offline: offline,
		http: &http.Client{Timeout: fetchTimeout},
	}
}

// Offline reports whether fetching is disabled.
func (f *HTTPFetcher) Offline() bool { return f.offline }

// target maps a stored cover URL to the URL actually requested.
func (f *HTTPFetcher) target(src string) (string, error) {
	u := src
	isOL := strings.HasPrefix(src, OpenLibraryCoversBase+"/")
	if isOL && f.base != "" && f.base != OpenLibraryCoversBase {
		u = f.base + strings.TrimPrefix(src, OpenLibraryCoversBase)
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%w: unsupported cover url %q", ErrNoCover, src)
	}
	if isOL {
		// 404 instead of a 1×1 placeholder GIF when Open Library has no cover.
		q := parsed.Query()
		q.Set("default", "false")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String(), nil
}

// Fetch downloads src and validates it is a reasonably sized image.
func (f *HTTPFetcher) Fetch(ctx context.Context, src string) (model.Cover, error) {
	if f.offline {
		return model.Cover{}, ErrOffline
	}
	target, err := f.target(src)
	if err != nil {
		return model.Cover{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return model.Cover{}, err
	}
	req.Header.Set("User-Agent", "athenaeum-library/1.0")
	req.Header.Set("Accept", "image/*")
	resp, err := f.http.Do(req)
	if err != nil {
		return model.Cover{}, fmt.Errorf("fetch cover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return model.Cover{}, ErrNoCover
	}
	if resp.StatusCode != http.StatusOK {
		return model.Cover{}, fmt.Errorf("fetch cover: status %d", resp.StatusCode)
	}
	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ctype, "image/") {
		return model.Cover{}, fmt.Errorf("%w: content-type %q", ErrNoCover, ctype)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return model.Cover{}, fmt.Errorf("read cover: %w", err)
	}
	if len(data) > MaxBytes {
		return model.Cover{}, fmt.Errorf("%w: larger than %d bytes", ErrNoCover, MaxBytes)
	}
	if len(data) < minBytes {
		return model.Cover{}, fmt.Errorf("%w: placeholder image (%d bytes)", ErrNoCover, len(data))
	}
	sum := sha256.Sum256(data)
	return model.Cover{SourceURL: src, ContentType: ctype, Bytes: data, SHA256: hex.EncodeToString(sum[:])}, nil
}

// Service serves cached covers with read-through fetching.
type Service struct {
	store   Store
	fetcher Fetcher
	log     *slog.Logger
	now     func() time.Time

	group  singleflight.Group
	mu     sync.Mutex
	misses map[string]time.Time // source URL → time of the last failed fetch
}

// New builds the service.
func New(store Store, fetcher Fetcher, log *slog.Logger) *Service {
	return &Service{store: store, fetcher: fetcher, log: log, now: time.Now, misses: map[string]time.Time{}}
}

var errNotFound = model.New(model.CodeNotFound, "Cover not available.")

// Get returns the book's cover, fetching and storing it on a cache miss. NOT_FOUND
// when the book does not exist, has no cover_url, or upstream cannot supply one.
func (s *Service) Get(ctx context.Context, bookID uuid.UUID) (model.Cover, error) {
	src, cached, err := s.store.ForBook(ctx, bookID)
	if err != nil {
		return model.Cover{}, err
	}
	if src == "" {
		return model.Cover{}, errNotFound
	}
	if cached != nil {
		return *cached, nil
	}
	if s.recentlyMissed(src) {
		return model.Cover{}, errNotFound
	}
	v, err, _ := s.group.Do(src, func() (any, error) {
		// Detached from the request: a closed tab must not abort a fetch other
		// requests are waiting on.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
		defer cancel()
		return s.fetchAndStore(fctx, src)
	})
	if err != nil {
		if errors.Is(err, ErrNoCover) || errors.Is(err, ErrOffline) {
			return model.Cover{}, errNotFound
		}
		return model.Cover{}, model.Wrap(model.CodeNotFound, "Cover not available.", err)
	}
	return v.(model.Cover), nil
}

func (s *Service) fetchAndStore(ctx context.Context, src string) (model.Cover, error) {
	c, err := s.fetcher.Fetch(ctx, src)
	if err != nil {
		if !errors.Is(err, ErrOffline) {
			s.mu.Lock()
			s.misses[src] = s.now()
			s.mu.Unlock()
		}
		return model.Cover{}, err
	}
	if err := s.store.Upsert(ctx, c); err != nil {
		// Still serve the bytes; the next request retries the write.
		s.log.Warn("cover_store_failed", "source_url", src, "err", err)
	}
	return c, nil
}

func (s *Service) recentlyMissed(src string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.misses[src]
	if !ok {
		return false
	}
	if s.now().Sub(at) > missTTL {
		delete(s.misses, src)
		return false
	}
	return true
}

// WarmResult summarizes a warm-up run.
type WarmResult struct {
	Fetched, Missing, Failed int
	Pruned                   int64
}

// Warm prunes unreferenced covers, then fetches every uncached books.cover_url with
// the given concurrency. Per-cover failures are logged, never returned (docs/08 §6).
func (s *Service) Warm(ctx context.Context, workers int) (WarmResult, error) {
	var res WarmResult
	pruned, err := s.store.Prune(ctx)
	if err != nil {
		return res, err
	}
	res.Pruned = pruned
	urls, err := s.store.Uncached(ctx)
	if err != nil {
		return res, err
	}
	if workers < 1 {
		workers = DefaultWorkers
	}
	jobs := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(workers, max(len(urls), 1)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for src := range jobs {
				fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
				_, err := s.fetchAndStore(fctx, src)
				cancel()
				mu.Lock()
				switch {
				case err == nil:
					res.Fetched++
				case errors.Is(err, ErrOffline):
					res.Failed++
				case errors.Is(err, ErrNoCover):
					res.Missing++
					s.log.Info("cover_missing", "source_url", src)
				default:
					res.Failed++
					s.log.Warn("cover_fetch_failed", "source_url", src, "err", err)
				}
				mu.Unlock()
			}
		}()
	}
	for _, u := range urls {
		if ctx.Err() != nil {
			break
		}
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	return res, ctx.Err()
}
