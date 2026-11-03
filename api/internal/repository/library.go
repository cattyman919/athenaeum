package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// LibraryRepository implements library.Repository.
type LibraryRepository struct{ pool *pgxpool.Pool }

// NewLibraryRepository wraps a pool.
func NewLibraryRepository(pool *pgxpool.Pool) *LibraryRepository {
	return &LibraryRepository{pool: pool}
}

// BookAvailability reports whether the book exists and how many copies are available.
func (r *LibraryRepository) BookAvailability(ctx context.Context, bookID uuid.UUID) (exists bool, available int, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM books WHERE id = $1),
		       (SELECT count(*)::int FROM book_copies WHERE book_id = $1 AND status = 'available')`, bookID).Scan(&exists, &available)
	if err != nil {
		return false, 0, fmt.Errorf("book availability: %w", err)
	}
	return exists, available, nil
}

// HasEntry reports whether the user already has the book in their library.
func (r *LibraryRepository) HasEntry(ctx context.Context, userID, bookID uuid.UUID) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_library WHERE user_id = $1 AND book_id = $2)`,
		userID, bookID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("has entry: %w", err)
	}
	return ok, nil
}

// Upsert merges patch into the existing row (or creates it with defaultStatus) atomically.
func (r *LibraryRepository) Upsert(ctx context.Context, userID, bookID uuid.UUID, patch model.LibraryPatch, defaultStatus string) (model.LibraryEntry, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.LibraryEntry{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var e model.LibraryEntry
	var id uuid.UUID
	created := false
	err = tx.QueryRow(ctx, `SELECT id, status, is_favorite, progress_pct, rating, added_at, updated_at
		FROM user_library WHERE user_id = $1 AND book_id = $2 FOR UPDATE`, userID, bookID).
		Scan(&id, &e.Status, &e.IsFavorite, &e.ProgressPct, &e.Rating, &e.AddedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		created = true
		id = uuid.New()
		e.Status = defaultStatus
	} else if err != nil {
		return model.LibraryEntry{}, false, fmt.Errorf("load entry: %w", err)
	}

	if patch.Status != nil {
		e.Status = *patch.Status
	}
	if patch.IsFavorite != nil {
		e.IsFavorite = *patch.IsFavorite
	}
	apply := func(dst **int, o model.Optional[int]) {
		switch {
		case !o.Set:
		case o.Null:
			*dst = nil
		default:
			v := o.Value
			*dst = &v
		}
	}
	apply(&e.ProgressPct, patch.ProgressPct)
	apply(&e.Rating, patch.Rating)

	err = tx.QueryRow(ctx, `
		INSERT INTO user_library (id, user_id, book_id, status, is_favorite, progress_pct, rating)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (user_id, book_id) DO UPDATE
		SET status = EXCLUDED.status, is_favorite = EXCLUDED.is_favorite,
		    progress_pct = EXCLUDED.progress_pct, rating = EXCLUDED.rating
		RETURNING added_at, updated_at`,
		id, userID, bookID, e.Status, e.IsFavorite, e.ProgressPct, e.Rating).Scan(&e.AddedAt, &e.UpdatedAt)
	if err != nil {
		return model.LibraryEntry{}, false, fmt.Errorf("upsert entry: %w", err)
	}
	e.BookID = bookID
	if err := tx.Commit(ctx); err != nil {
		return model.LibraryEntry{}, false, err
	}
	return e, created, nil
}

// Delete removes the entry (idempotent).
func (r *LibraryRepository) Delete(ctx context.Context, userID, bookID uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM user_library WHERE user_id = $1 AND book_id = $2`, userID, bookID); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

// List returns one page of the user's library plus the unpaginated total.
func (r *LibraryRepository) List(ctx context.Context, userID uuid.UUID, f model.LibraryFilter, page, perPage int) ([]model.LibraryItem, int, error) {
	args := []any{userID}
	where := "ul.user_id = $1"
	if len(f.Statuses) > 0 || f.Favorites {
		args = append(args, f.Statuses, f.Favorites)
		where += fmt.Sprintf(" AND (ul.status = ANY($%d) OR ($%d AND ul.is_favorite))", len(args)-1, len(args))
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM user_library ul WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count library: %w", err)
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT b.id, b.title, `+coverURLSQL("b")+`, b.authors, ul.status, ul.is_favorite, ul.progress_pct, ul.rating, ul.added_at,
		       loc.floor_number, loc.shelf_code, loc.path
		FROM user_library ul JOIN books b ON b.id = ul.book_id
		LEFT JOIN LATERAL (
		  SELECT floor_number, shelf_code, path FROM book_locations
		  WHERE book_id = b.id AND path IS NOT NULL
		  ORDER BY (copy_status = 'available') DESC, barcode LIMIT 1) loc ON true
		WHERE %s ORDER BY ul.updated_at DESC, b.title LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list library: %w", err)
	}
	defer rows.Close()
	out := []model.LibraryItem{}
	for rows.Next() {
		it := model.LibraryItem{Kind: "entry"}
		var authors []byte
		var floor *int
		var shelf, path *string
		if err := rows.Scan(&it.Book.ID, &it.Book.Title, &it.Book.CoverURL, &authors, &it.Status, &it.IsFavorite,
			&it.ProgressPct, &it.Rating, &it.AddedAt, &floor, &shelf, &path); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(authors, &it.Book.Authors); err != nil {
			return nil, 0, err
		}
		if it.Book.Authors == nil {
			it.Book.Authors = []string{}
		}
		if path != nil {
			it.Location = &model.LibraryLocation{Floor: derefInt(floor), ShelfCode: deref(shelf), Path: *path}
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// Counts returns the KPI numbers.
func (r *LibraryRepository) Counts(ctx context.Context, userID uuid.UUID) (model.LibraryCounts, error) {
	var c model.LibraryCounts
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       (count(*) FILTER (WHERE status = 'reading'))::int,
		       (count(*) FILTER (WHERE status = 'read'))::int,
		       (count(*) FILTER (WHERE status = 'want_to_read'))::int,
		       (count(*) FILTER (WHERE is_favorite))::int
		FROM user_library WHERE user_id = $1`, userID).Scan(&c.Total, &c.Reading, &c.Read, &c.WantToRead, &c.Favorites)
	if err != nil {
		return model.LibraryCounts{}, fmt.Errorf("library counts: %w", err)
	}
	return c, nil
}
