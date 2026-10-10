// Package olbooks is the Open Library client with fallback semantics
// (docs/08 §6).
package olbooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrNotFound means Open Library has no record for the ISBN.
	ErrNotFound = errors.New("olbooks: not found")
	// ErrUnavailable means the lookup was skipped (offline mode) or all
	// attempts failed; callers fall back to local values.
	ErrUnavailable = errors.New("olbooks: upstream unavailable")
	// ErrOffline is ErrUnavailable returned deliberately by OFFLINE_MODE / --offline.
	ErrOffline = fmt.Errorf("%w (offline mode)", ErrUnavailable)
)

// Metadata is the normalized Open Library record.
type Metadata struct {
	Title       string
	Authors     []string
	Publisher   string
	Year        int
	Dewey       string
	LOC         string
	Key         string // e.g. "/books/OL7353617M"
	CoverURL    string
	Description string
}

// CoverURLFor builds the deterministic cover URL (docs/08 §6).
func CoverURLFor(isbn13 string) string {
	return "https://covers.openlibrary.org/b/isbn/" + isbn13 + "-L.jpg"
}

// Client talks to Open Library.
type Client struct {
	base    string
	http    *http.Client
	offline bool
	retries int
	backoff time.Duration
}

// New builds a client: 3 s timeout, 2 retries, 500 ms backoff (docs/08 §6).
// With offline=true no network calls are made.
func New(baseURL string, offline bool) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 3 * time.Second},
		offline: offline, retries: 2, backoff: 500 * time.Millisecond,
	}
}

// WithRetry overrides retry behavior (tests).
func (c *Client) WithRetry(retries int, backoff time.Duration) *Client {
	cp := *c
	cp.retries, cp.backoff = retries, backoff
	return &cp
}

// Lookup fetches metadata by ISBN-13.
func (c *Client) Lookup(ctx context.Context, isbn13 string) (Metadata, error) {
	if c.offline {
		return Metadata{}, ErrOffline
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Metadata{}, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
			case <-time.After(c.backoff):
			}
		}
		md, err := c.fetch(ctx, isbn13)
		if err == nil || errors.Is(err, ErrNotFound) {
			return md, err
		}
		lastErr = err
	}
	return Metadata{}, fmt.Errorf("%w: %w", ErrUnavailable, lastErr)
}

type olRecord struct {
	Title       string                  `json:"title"`
	Authors     []struct{ Name string } `json:"authors"`
	Publishers  []struct{ Name string } `json:"publishers"`
	PublishDate string                  `json:"publish_date"`
	Key         string                  `json:"key"`
	Notes       any                     `json:"notes"`
	Cover       struct {
		Large string `json:"large"`
	} `json:"cover"`
	Classifications struct {
		Dewey []string `json:"dewey_decimal_class"`
		LC    []string `json:"lc_classifications"`
	} `json:"classifications"`
}

func (c *Client) fetch(ctx context.Context, isbn13 string) (Metadata, error) {
	q := url.Values{"bibkeys": {"ISBN:" + isbn13}, "jscmd": {"data"}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/books?"+q.Encode(), nil)
	if err != nil {
		return Metadata{}, err
	}
	req.Header.Set("User-Agent", "athenaeum-library/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return Metadata{}, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Metadata{}, fmt.Errorf("open library status %d", resp.StatusCode)
	}
	var body map[string]olRecord
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Metadata{}, fmt.Errorf("decode: %w", err)
	}
	rec, ok := body["ISBN:"+isbn13]
	if !ok || rec.Title == "" {
		return Metadata{}, ErrNotFound
	}
	md := Metadata{Title: rec.Title, Key: rec.Key, CoverURL: rec.Cover.Large}
	if md.CoverURL == "" {
		md.CoverURL = CoverURLFor(isbn13)
	}
	for _, a := range rec.Authors {
		md.Authors = append(md.Authors, a.Name)
	}
	if len(rec.Publishers) > 0 {
		md.Publisher = rec.Publishers[0].Name
	}
	if len(rec.Classifications.Dewey) > 0 {
		md.Dewey = rec.Classifications.Dewey[0]
	}
	if len(rec.Classifications.LC) > 0 {
		md.LOC = rec.Classifications.LC[0]
	}
	md.Year = parseYear(rec.PublishDate)
	if s, ok := rec.Notes.(string); ok {
		md.Description = s
	}
	return md, nil
}

func parseYear(s string) int {
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '-' || r == '/' }) {
		if len(f) == 4 {
			if y, err := strconv.Atoi(f); err == nil {
				return y
			}
		}
	}
	return 0
}
