package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/hash"
	"github.com/cattyman919/athenaeum/api/internal/service/olbooks"
)

// Store is the persistence the seeder needs (implemented by repository.SeedStore).
type Store interface {
	// Reset truncates catalog content (never users/refresh_tokens).
	Reset(ctx context.Context) error
	// UpsertUser inserts or updates by lower(email).
	UpsertUser(ctx context.Context, u model.User) error
	// ApplyBuilding upserts the tree by natural keys, one transaction per floor.
	ApplyBuilding(ctx context.Context, b model.BuildingFixture) error
	// ApplyBook upserts one book and its copies in a single transaction;
	// bookIndex is 1-based (barcode allocation) and shelf paths are resolved
	// with building.ResolvePath.
	ApplyBook(ctx context.Context, bookIndex int, b model.BookFixture) error
	// Counts returns the database totals after seeding.
	Counts(ctx context.Context) (model.SeedCounts, error)
}

// Enricher looks up Open Library metadata (olbooks.Client).
type Enricher interface {
	Lookup(ctx context.Context, isbn13 string) (olbooks.Metadata, error)
}

// UserSpec describes a seeded account.
type UserSpec struct {
	Email, Name, Password, Role string
}

// Options configure Run.
type Options struct {
	Reset bool
	Users []UserSpec
}

// Run applies the fixtures: users → tree → books/copies (docs/08 §4).
// Fixtures must already be validated.
func Run(ctx context.Context, st Store, enr Enricher, fx Fixtures, opts Options, log *slog.Logger) (model.SeedCounts, error) {
	if opts.Reset {
		if err := st.Reset(ctx); err != nil {
			return model.SeedCounts{}, fmt.Errorf("reset: %w", err)
		}
	}
	for _, u := range opts.Users {
		ph, err := hash.Hash(u.Password)
		if err != nil {
			return model.SeedCounts{}, err
		}
		if err := st.UpsertUser(ctx, model.User{Email: u.Email, Name: u.Name, Role: u.Role, PasswordHash: &ph}); err != nil {
			return model.SeedCounts{}, fmt.Errorf("user %s: %w", u.Email, err)
		}
	}
	if err := st.ApplyBuilding(ctx, fx.Building); err != nil {
		return model.SeedCounts{}, fmt.Errorf("building: %w", err)
	}
	for i, b := range fx.Books {
		b = enrich(ctx, enr, b, log)
		if err := st.ApplyBook(ctx, i+1, b); err != nil {
			return model.SeedCounts{}, fmt.Errorf("books[%d] (%s): %w", i, b.Title, err)
		}
	}
	return st.Counts(ctx)
}

// enrich fills blanks from Open Library; mock values always win and failures
// fall back silently (docs/08 §6).
func enrich(ctx context.Context, enr Enricher, b model.BookFixture, log *slog.Logger) model.BookFixture {
	if b.ISBN13 == "" {
		return b
	}
	b.CoverURL = olbooks.CoverURLFor(b.ISBN13)
	if enr == nil {
		return b
	}
	md, err := enr.Lookup(ctx, b.ISBN13)
	switch {
	case err == nil:
		if b.Publisher == "" {
			b.Publisher = md.Publisher
		}
		if b.PublishedYear == 0 {
			b.PublishedYear = md.Year
		}
		if b.DeweyNum == "" {
			b.DeweyNum = md.Dewey
		}
		if b.LOCNum == "" {
			b.LOCNum = md.LOC
		}
		if b.OpenLibraryKey == nil && md.Key != "" {
			key := md.Key
			b.OpenLibraryKey = &key
		}
		if md.CoverURL != "" {
			b.CoverURL = md.CoverURL
		}
	case errors.Is(err, olbooks.ErrNotFound):
		// keep mock values; open_library_key stays NULL
	case errors.Is(err, olbooks.ErrOffline):
		// deliberate: no network lookups in offline mode
	default:
		log.Warn("enrich_failed", "isbn13", b.ISBN13, "err", err)
	}
	return b
}
