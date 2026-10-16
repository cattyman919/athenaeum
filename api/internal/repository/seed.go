package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/building"
)

// SeedStore implements seed.Store.
type SeedStore struct{ pool *pgxpool.Pool }

// NewSeedStore wraps a pool.
func NewSeedStore(pool *pgxpool.Pool) *SeedStore { return &SeedStore{pool: pool} }

// Reset truncates catalog content, children first; users/refresh_tokens stay.
func (s *SeedStore) Reset(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE loans, book_copies, copy_events, book_embeddings, user_library, waitlist,
		books, conversations, messages, shelves, aisles, sections, floors, buildings RESTART IDENTITY`)
	if err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	return nil
}

// UpsertUser inserts or updates by lower(email).
func (s *SeedStore) UpsertUser(ctx context.Context, u model.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role) VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT ((lower(email)::text)) DO UPDATE
		 SET password_hash = EXCLUDED.password_hash, name = EXCLUDED.name, role = EXCLUDED.role`,
		uuid.New(), u.Email, u.PasswordHash, u.Name, u.Role)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// upsertID runs an INSERT … ON CONFLICT … RETURNING id.
func upsertID(ctx context.Context, tx pgx.Tx, sql string, args ...any) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// ApplyBuilding upserts the tree by natural keys, one transaction per floor.
func (s *SeedStore) ApplyBuilding(ctx context.Context, b model.BuildingFixture) error {
	var buildingID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO buildings (id, code, name, address)
		 SELECT $1,$2,$3,$4 WHERE NOT EXISTS (SELECT 1 FROM buildings WHERE code = $2)
		 RETURNING id`, uuid.New(), b.Building.Code, b.Building.Name, b.Building.Address).Scan(&buildingID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = s.pool.QueryRow(ctx,
			`UPDATE buildings SET name = $2, address = $3 WHERE code = $1 RETURNING id`,
			b.Building.Code, b.Building.Name, b.Building.Address).Scan(&buildingID); err != nil {
			return fmt.Errorf("update building: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("insert building: %w", err)
	}

	for _, fl := range b.Floors {
		if err := s.applyFloor(ctx, buildingID, fl); err != nil {
			return fmt.Errorf("floor %d: %w", fl.FloorNumber, err)
		}
	}
	return nil
}

func (s *SeedStore) applyFloor(ctx context.Context, buildingID uuid.UUID, fl model.FloorFixture) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	floorID, err := upsertID(ctx, tx,
		`INSERT INTO floors (id, building_id, floor_number, name) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (building_id, floor_number) DO UPDATE SET name = EXCLUDED.name RETURNING id`,
		uuid.New(), buildingID, fl.FloorNumber, fl.Name)
	if err != nil {
		return err
	}
	for _, sec := range fl.Sections {
		secID, err := upsertID(ctx, tx,
			`INSERT INTO sections (id, floor_id, code, name, color) VALUES ($1,$2,$3,$4,$5)
			 ON CONFLICT (floor_id, code) DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color RETURNING id`,
			uuid.New(), floorID, sec.Code, sec.Name, sec.Color)
		if err != nil {
			return err
		}
		for _, ai := range sec.Aisles {
			aisleID, err := upsertID(ctx, tx,
				`INSERT INTO aisles (id, section_id, code) VALUES ($1,$2,$3)
				 ON CONFLICT (section_id, code) DO UPDATE SET code = EXCLUDED.code RETURNING id`,
				uuid.New(), secID, ai.Code)
			if err != nil {
				return err
			}
			for _, sh := range ai.Shelves {
				if _, err := tx.Exec(ctx,
					`INSERT INTO shelves (id, aisle_id, code, row_level, height_tier) VALUES ($1,$2,$3,$4,$5)
					 ON CONFLICT (aisle_id, code) DO UPDATE SET row_level = EXCLUDED.row_level, height_tier = EXCLUDED.height_tier`,
					uuid.New(), aisleID, sh.Code, sh.RowLevel, sh.HeightTier); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit(ctx)
}

// ApplyBook upserts one book and its copies atomically.
func (s *SeedStore) ApplyBook(ctx context.Context, index int, b model.BookFixture) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sectionID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT se.id FROM sections se WHERE se.code = $1`, b.SectionCode).Scan(&sectionID); err != nil {
		return fmt.Errorf("section %q: %w", b.SectionCode, err)
	}
	authors, err := json.Marshal(b.Authors)
	if err != nil {
		return err
	}

	// Natural key: isbn13, else title + first author.
	var bookID uuid.UUID
	var lookupErr error
	if b.ISBN13 != "" {
		lookupErr = tx.QueryRow(ctx, `SELECT id FROM books WHERE isbn13 = $1`, b.ISBN13).Scan(&bookID)
	} else {
		first := ""
		if len(b.Authors) > 0 {
			first = b.Authors[0]
		}
		lookupErr = tx.QueryRow(ctx, `SELECT id FROM books WHERE title = $1 AND coalesce(authors->>0,'') = $2`, b.Title, first).Scan(&bookID)
	}
	args := []any{b.Title, b.Subtitle, authors, nullable(b.ISBN13), nullable(b.Publisher), nullableInt(b.PublishedYear),
		nullable(b.Description), nullable(b.CoverURL), b.OpenLibraryKey, nullable(b.DeweyNum), nullable(b.LOCNum), sectionID}
	switch {
	case errors.Is(lookupErr, pgx.ErrNoRows):
		bookID = uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO books (id, title, subtitle, authors, isbn13, publisher, published_year, description,
			   cover_url, open_library_key, dewey_num, loc_num, section_id)
			 VALUES ($13,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, append(args, bookID)...); err != nil {
			return fmt.Errorf("insert book: %w", err)
		}
	case lookupErr != nil:
		return fmt.Errorf("find book: %w", lookupErr)
	default:
		if _, err := tx.Exec(ctx,
			`UPDATE books SET title=$1, subtitle=$2, authors=$3, isbn13=$4, publisher=$5, published_year=$6, description=$7,
			   cover_url=$8, open_library_key=COALESCE($9, open_library_key), dewey_num=$10, loc_num=$11, section_id=$12 WHERE id=$13`, append(args, bookID)...); err != nil {
			return fmt.Errorf("update book: %w", err)
		}
	}

	resolver := NewBuildingRepository(tx)
	for n, c := range b.Copies {
		shelfID, err := building.ResolvePath(ctx, resolver, c.ShelfPath)
		if err != nil {
			return fmt.Errorf("copies[%d]: %w", n, err)
		}
		barcode := c.Barcode
		if barcode == "" {
			barcode = fmt.Sprintf("ATH-%06d-C%02d", 2940+index, n+1)
		}
		condition := c.Condition
		if condition == "" {
			condition = "good"
		}
		var copyID uuid.UUID
		var inserted bool
		err = tx.QueryRow(ctx,
			`INSERT INTO book_copies (id, book_id, barcode, status, condition, shelf_id)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 ON CONFLICT (barcode) DO UPDATE SET book_id = EXCLUDED.book_id, condition = EXCLUDED.condition,
			   shelf_id = EXCLUDED.shelf_id, status = EXCLUDED.status,
			   last_state_change = CASE WHEN book_copies.status <> EXCLUDED.status THEN now() ELSE book_copies.last_state_change END
			 RETURNING id, (xmax = 0)`,
			uuid.New(), bookID, barcode, c.Status, condition, shelfID).Scan(&copyID, &inserted)
		if err != nil {
			return fmt.Errorf("copies[%d] %s: %w", n, barcode, err)
		}
		if inserted {
			if _, err := tx.Exec(ctx,
				`INSERT INTO copy_events (copy_id, from_status, to_status, source) VALUES ($1, NULL, $2, 'seed')`,
				copyID, c.Status); err != nil {
				return fmt.Errorf("copy event: %w", err)
			}
		}
	}
	return tx.Commit(ctx)
}

// ApplyDemoLibrary seeds the demo patron's library and holds idempotently.
func (s *SeedStore) ApplyDemoLibrary(ctx context.Context, l model.LibraryFixture) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email) = lower($1)`, l.UserEmail).Scan(&userID); err != nil {
		return fmt.Errorf("demo user %s: %w", l.UserEmail, err)
	}
	bookByTitle := func(title string) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM books WHERE title = $1`, title).Scan(&id)
		if err != nil {
			return uuid.Nil, fmt.Errorf("book %q: %w", title, err)
		}
		return id, nil
	}
	for _, e := range l.Entries {
		bookID, err := bookByTitle(e.Title)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_library (id, user_id, book_id, status, is_favorite, progress_pct, rating)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (user_id, book_id) DO UPDATE
			SET status = EXCLUDED.status, is_favorite = EXCLUDED.is_favorite,
			    progress_pct = EXCLUDED.progress_pct, rating = EXCLUDED.rating`,
			uuid.New(), userID, bookID, e.Status, e.IsFavorite, e.ProgressPct, e.Rating); err != nil {
			return fmt.Errorf("library entry %q: %w", e.Title, err)
		}
	}
	for _, title := range l.Waitlist {
		bookID, err := bookByTitle(title)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO waitlist (id, book_id, user_id)
			SELECT $1, $2, $3 WHERE NOT EXISTS (
			  SELECT 1 FROM waitlist WHERE book_id = $2 AND user_id = $3 AND status IN ('waiting','ready'))`,
			uuid.New(), bookID, userID); err != nil {
			return fmt.Errorf("waitlist %q: %w", title, err)
		}
	}
	return tx.Commit(ctx)
}

// Counts returns database totals.
func (s *SeedStore) Counts(ctx context.Context) (model.SeedCounts, error) {
	var c model.SeedCounts
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users),
		(SELECT count(*) FROM books),
		(SELECT count(*) FROM book_copies),
		(SELECT count(*) FROM shelves),
		(SELECT count(*) FROM book_copies WHERE status = 'checked_out'),
		(SELECT count(*) FROM books b WHERE NOT EXISTS (SELECT 1 FROM book_copies bc WHERE bc.book_id = b.id)),
		(SELECT count(*) FROM user_library),
		(SELECT count(*) FROM waitlist WHERE status IN ('waiting','ready')),
		(SELECT count(*) FROM loans WHERE returned_at IS NULL)`).
		Scan(&c.Users, &c.Books, &c.Copies, &c.Shelves, &c.CheckedOut, &c.ZeroCopyBooks, &c.LibraryEntries, &c.WaitlistRows, &c.Loans)
	if err != nil {
		return model.SeedCounts{}, fmt.Errorf("counts: %w", err)
	}
	return c, nil
}

// ApplyLoans creates one open loan per checked_out copy, cycling through the
// 20 reviewer accounts (user1@gmail.com…user20@gmail.com, docs/08 §4, docs/13).
// Idempotent: ON CONFLICT DO NOTHING on the partial unique index
// loans_open_copy_key (copy_id WHERE returned_at IS NULL).
func (s *SeedStore) ApplyLoans(ctx context.Context, now time.Time) error {
	// Fetch reviewer user IDs sorted by email.
	reviewerRows, err := s.pool.Query(ctx,
		`SELECT id FROM users WHERE email ~* '^user[0-9]+@gmail\.com$' ORDER BY lower(email)`)
	if err != nil {
		return fmt.Errorf("reviewer users: %w", err)
	}
	var reviewerIDs []uuid.UUID
	for reviewerRows.Next() {
		var id uuid.UUID
		if err := reviewerRows.Scan(&id); err != nil {
			reviewerRows.Close()
			return fmt.Errorf("scan reviewer: %w", err)
		}
		reviewerIDs = append(reviewerIDs, id)
	}
	reviewerRows.Close()
	if err := reviewerRows.Err(); err != nil {
		return fmt.Errorf("reviewer rows: %w", err)
	}
	if len(reviewerIDs) == 0 {
		return nil // reviewer accounts not yet seeded; skip
	}

	// Fetch checked_out copies ordered by barcode.
	copyRows, err := s.pool.Query(ctx,
		`SELECT id FROM book_copies WHERE status = 'checked_out' ORDER BY barcode`)
	if err != nil {
		return fmt.Errorf("checked_out copies: %w", err)
	}
	var copyIDs []uuid.UUID
	for copyRows.Next() {
		var id uuid.UUID
		if err := copyRows.Scan(&id); err != nil {
			copyRows.Close()
			return fmt.Errorf("scan copy: %w", err)
		}
		copyIDs = append(copyIDs, id)
	}
	copyRows.Close()
	if err := copyRows.Err(); err != nil {
		return fmt.Errorf("copy rows: %w", err)
	}

	dueAt := now.Add(model.LoanPeriod)
	for i, copyID := range copyIDs {
		reviewerID := reviewerIDs[i%len(reviewerIDs)]
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO loans (id, copy_id, user_id, borrowed_at, due_at, renewals)
			VALUES ($1,$2,$3,$4,$5,0)
			ON CONFLICT DO NOTHING`,
			uuid.New(), copyID, reviewerID, now, dueAt); err != nil {
			return fmt.Errorf("insert loan for copy %s: %w", copyID, err)
		}
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
