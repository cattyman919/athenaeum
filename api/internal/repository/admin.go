package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

const (
	pgForeignKeyViolation = "23503"
	// accessionBase: book #n of the catalog owns accession 002940+n (docs/08 §5).
	accessionBase = 2940
	// accessionLockKey serializes barcode allocation across transactions.
	accessionLockKey = 2940_0001
	maxCopyNumber    = 99
)

// AdminRepository backs the admin endpoints (docs/03 §8).
type AdminRepository struct{ pool *pgxpool.Pool }

// NewAdminRepository wraps a pool.
func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository { return &AdminRepository{pool: pool} }

// ---- KPIs & audit ----------------------------------------------------------

// KPIs computes the GET /api/admin/data snapshot. Active patrons are `user`-role accounts
// with a session or library activity in the trailing 30 days; chat counts cover 7 days.
func (r *AdminRepository) KPIs(ctx context.Context, now time.Time) (model.AdminKPIs, error) {
	k := model.AdminKPIs{ComputedAt: now.UTC()}
	var shelves int
	err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM books),
		  (SELECT count(*) FROM book_copies),
		  (SELECT count(*) FROM book_copies WHERE status = 'available'),
		  (SELECT count(*) FROM book_copies WHERE status = 'checked_out'),
		  (SELECT count(*) FROM book_copies WHERE status = 'missing_lost'),
		  (SELECT count(*) FROM book_copies WHERE shelf_id IS NOT NULL),
		  (SELECT count(*) FROM shelves),
		  (SELECT count(*) FROM users u WHERE u.role = 'user' AND (
		     EXISTS (SELECT 1 FROM refresh_tokens t WHERE t.user_id = u.id AND t.created_at > $1)
		     OR EXISTS (SELECT 1 FROM user_library l WHERE l.user_id = u.id AND l.updated_at > $1))),
		  (SELECT count(*) FROM conversations WHERE updated_at > $2),
		  (SELECT count(*) FROM messages WHERE created_at > $2)`,
		now.Add(-30*24*time.Hour), now.Add(-7*24*time.Hour),
	).Scan(&k.TitlesInCatalog, &k.Copies.Total, &k.Copies.Available, &k.Copies.CheckedOut, &k.Copies.MissingLost,
		&k.ShelfCapacity.Occupied, &shelves, &k.ActivePatrons, &k.ChatWeek.Conversations, &k.ChatWeek.Messages)
	if err != nil {
		return model.AdminKPIs{}, fmt.Errorf("admin kpis: %w", err)
	}
	k.ShelfCapacity.Total = shelves * model.ShelfSlots
	return k, nil
}

// CopyEvents returns up to limit audit rows newest first; with since set only rows
// created strictly after it.
func (r *AdminRepository) CopyEvents(ctx context.Context, since *time.Time, limit int) ([]model.CopyEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, bc.barcode, b.title, e.from_status, e.to_status, e.source, u.id, u.name, e.created_at
		FROM copy_events e
		JOIN book_copies bc ON bc.id = e.copy_id
		JOIN books b ON b.id = bc.book_id
		LEFT JOIN users u ON u.id = e.actor_id
		WHERE $1::timestamptz IS NULL OR e.created_at > $1
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("copy events: %w", err)
	}
	defer rows.Close()
	out := []model.CopyEvent{}
	for rows.Next() {
		var e model.CopyEvent
		var actorID *uuid.UUID
		var actorName *string
		if err := rows.Scan(&e.ID, &e.Barcode, &e.BookTitle, &e.FromStatus, &e.ToStatus, &e.Source,
			&actorID, &actorName, &e.CreatedAt); err != nil {
			return nil, err
		}
		if actorID != nil {
			e.Actor = &model.ActorRef{ID: *actorID, Name: deref(actorName)}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- books -----------------------------------------------------------------

var adminSortCols = map[string]string{
	"updated_at": "b.updated_at", "created_at": "b.created_at", "title": "b.title", "published_year": "b.published_year",
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ListBooks pages books matching the filter (title/author/ISBN substring, shelf).
func (r *AdminRepository) ListBooks(ctx context.Context, f model.AdminBookFilter, page, perPage int) ([]model.AdminBookRow, int, error) {
	col, ok := adminSortCols[f.SortCol]
	if !ok {
		col = "b.updated_at"
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	where := `($1 = '' OR b.title ILIKE '%' || $1 || '%' OR b.authors::text ILIKE '%' || $1 || '%' OR b.isbn13 LIKE '%' || $1 || '%' OR EXISTS (SELECT 1 FROM book_isbns i WHERE i.book_id = b.id AND i.isbn13 LIKE '%' || $1 || '%'))
		AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM book_copies c WHERE c.book_id = b.id AND c.shelf_id = $2))`
	search := likeEscaper.Replace(strings.TrimSpace(f.Search))
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM books b WHERE `+where, search, f.ShelfID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin books: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT b.id, b.open_library_key, b.cover_url, b.updated_at, (SELECT count(*) FROM loans l JOIN book_copies c ON c.id = l.copy_id WHERE c.book_id = b.id AND l.returned_at IS NULL) FROM books b WHERE `+where+
		` ORDER BY `+col+` `+dir+` NULLS LAST, b.id LIMIT $3 OFFSET $4`,
		search, f.ShelfID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("admin books: %w", err)
	}
	defer rows.Close()
	out := []model.AdminBookRow{}
	for rows.Next() {
		var b model.AdminBookRow
		if err := rows.Scan(&b.ID, &b.Meta.OpenLibraryKey, &b.Meta.CoverURL, &b.Meta.UpdatedAt, &b.ActiveLoans); err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

// BookMeta loads admin-only book metadata; NOT_FOUND when absent.
func (r *AdminRepository) BookMeta(ctx context.Context, id uuid.UUID) (model.BookMeta, error) {
	var m model.BookMeta
	err := r.pool.QueryRow(ctx, `SELECT b.open_library_key, b.cover_url, b.updated_at,
		(SELECT count(*) FROM loans l JOIN book_copies c ON c.id = l.copy_id WHERE c.book_id = b.id AND l.returned_at IS NULL)
		FROM books b WHERE b.id = $1`, id).
		Scan(&m.OpenLibraryKey, &m.CoverURL, &m.UpdatedAt, &m.ActiveLoans)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.BookMeta{}, model.New(model.CodeNotFound, "Book not found.")
	}
	if err != nil {
		return model.BookMeta{}, fmt.Errorf("book meta: %w", err)
	}
	return m, nil
}

// BookIDByISBN finds a book by ISBN-13 via book_isbns (plan M21).
func (r *AdminRepository) BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT book_id FROM book_isbns WHERE isbn13 = $1`, isbn13).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("book by isbn: %w", err)
	}
	return id, true, nil
}

// ISBNsForBook returns all ISBNs for a book from book_isbns, primary first.
func (r *AdminRepository) ISBNsForBook(ctx context.Context, bookID uuid.UUID) ([]model.ISBNRef, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT isbn13, is_primary FROM book_isbns WHERE book_id = $1 ORDER BY is_primary DESC, isbn13`, bookID)
	if err != nil {
		return nil, fmt.Errorf("isbns for book: %w", err)
	}
	defer rows.Close()
	var out []model.ISBNRef
	for rows.Next() {
		var ref model.ISBNRef
		if err := rows.Scan(&ref.ISBN13, &ref.IsPrimary); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	if out == nil {
		out = []model.ISBNRef{}
	}
	return out, rows.Err()
}

// AttachISBN adds an additional edition ISBN to a book. Returns ISBN_CONFLICT (409) if the
// ISBN is already owned by any book (including this one, so callers know it's a duplicate).
func (r *AdminRepository) AttachISBN(ctx context.Context, bookID uuid.UUID, isbn13 string) ([]model.ISBNRef, error) {
	// Check ownership.
	ownerID, found, err := r.BookIDByISBN(ctx, isbn13)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, model.New(model.CodeISBNConflict, "This ISBN is already attached to a book.").
			WithExtra(map[string]any{"book_id": ownerID.String()})
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ($1, $2, false)`,
		isbn13, bookID); err != nil {
		return nil, fmt.Errorf("attach isbn: %w", err)
	}
	return r.ISBNsForBook(ctx, bookID)
}

// DetachISBN removes a non-primary ISBN from a book. Returns CONFLICT if it is primary,
// VALIDATION_FAILED if it does not belong to this book.
func (r *AdminRepository) DetachISBN(ctx context.Context, bookID uuid.UUID, isbn13 string) error {
	var isPrimary bool
	var ownerID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT book_id, is_primary FROM book_isbns WHERE isbn13 = $1`, isbn13).Scan(&ownerID, &isPrimary)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.New(model.CodeNotFound, "ISBN not found.")
	}
	if err != nil {
		return fmt.Errorf("detach isbn lookup: %w", err)
	}
	if ownerID != bookID {
		return model.New(model.CodeValidationFailed, "ISBN does not belong to this book.")
	}
	if isPrimary {
		return model.New(model.CodeConflict, "Cannot remove the primary ISBN. Update books.isbn13 instead.")
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM book_isbns WHERE isbn13 = $1`, isbn13); err != nil {
		return fmt.Errorf("detach isbn: %w", err)
	}
	return nil
}

// MergeBooks merges source book into target in one transaction (plan M21):
//   - Repoints book_copies (copy tags unchanged)
//   - Upserts user_library keeping higher status/rating
//   - Moves waitlist entries (conflict → keep target's)
//   - Moves all source ISBNs as non-primary to target
//   - Deletes the source book
//   - Sets target embedding_dirty = true
//   - Appends one copy_event per moved copy (source admin_patch)
//   - Appends one audit row
func (r *AdminRepository) MergeBooks(ctx context.Context, actor, sourceID, targetID uuid.UUID, now time.Time) (int, []model.ISBNRef, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock both rows in consistent order to prevent deadlocks.
	first, second := sourceID, targetID
	if first.String() > second.String() {
		first, second = second, first
	}
	var sourceExists, targetExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM books WHERE id=$1), EXISTS(SELECT 1 FROM books WHERE id=$2)`,
		sourceID, targetID).Scan(&sourceExists, &targetExists); err != nil {
		return 0, nil, fmt.Errorf("check books: %w", err)
	}
	if !sourceExists {
		return 0, nil, model.New(model.CodeNotFound, "Source book not found.")
	}
	if !targetExists {
		return 0, nil, model.New(model.CodeNotFound, "Target book not found.")
	}
	// Lock both.
	if _, err := tx.Exec(ctx, `SELECT id FROM books WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, first, second); err != nil {
		return 0, nil, fmt.Errorf("lock books: %w", err)
	}

	// Move book_copies and track how many.
	var movedCopies int
	err = tx.QueryRow(ctx,
		`WITH moved AS (
			UPDATE book_copies SET book_id = $2 WHERE book_id = $1 RETURNING id
		) SELECT count(*) FROM moved`, sourceID, targetID).Scan(&movedCopies)
	if err != nil {
		return 0, nil, fmt.Errorf("move copies: %w", err)
	}

	// Move copy events — they reference copy_id, not book_id; nothing to do.

	// Merge user_library: keep target row but take higher status/rating.
	// On conflict: take the higher-priority status; OR the is_favorite flags; take the higher rating.
	// Status priority (highest first): reading(4) > read(3) > want_to_read(2) > dropped(1) > other(0).
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_library (id, user_id, book_id, status, is_favorite, rating, progress_pct)
		SELECT id, user_id, $2, status, is_favorite, rating, progress_pct
		FROM user_library WHERE book_id = $1
		ON CONFLICT (user_id, book_id) DO UPDATE
		  SET status = CASE
		        WHEN (CASE EXCLUDED.status WHEN 'reading' THEN 4 WHEN 'read' THEN 3 WHEN 'want_to_read' THEN 2 WHEN 'dropped' THEN 1 ELSE 0 END)
		             > (CASE user_library.status WHEN 'reading' THEN 4 WHEN 'read' THEN 3 WHEN 'want_to_read' THEN 2 WHEN 'dropped' THEN 1 ELSE 0 END)
		        THEN EXCLUDED.status
		        ELSE user_library.status
		      END,
		      is_favorite = user_library.is_favorite OR EXCLUDED.is_favorite,
		      rating      = GREATEST(user_library.rating, EXCLUDED.rating)`,
		sourceID, targetID); err != nil {
		return 0, nil, fmt.Errorf("merge library: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_library WHERE book_id = $1`, sourceID); err != nil {
		return 0, nil, fmt.Errorf("delete source library: %w", err)
	}

	// Merge waitlist: move rows that don't conflict; drop source rows that do.
	if _, err := tx.Exec(ctx, `
		UPDATE waitlist SET book_id = $2
		WHERE book_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM waitlist w2 WHERE w2.book_id = $2 AND w2.user_id = waitlist.user_id
		  )`, sourceID, targetID); err != nil {
		return 0, nil, fmt.Errorf("merge waitlist: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM waitlist WHERE book_id = $1`, sourceID); err != nil {
		return 0, nil, fmt.Errorf("delete source waitlist: %w", err)
	}

	// Move all source ISBNs as non-primary to target (primary stays unique on target).
	if _, err := tx.Exec(ctx,
		`UPDATE book_isbns SET book_id = $2, is_primary = false WHERE book_id = $1`,
		sourceID, targetID); err != nil {
		return 0, nil, fmt.Errorf("move isbns: %w", err)
	}

	// Delete source book.
	if _, err := tx.Exec(ctx, `DELETE FROM books WHERE id = $1`, sourceID); err != nil {
		return 0, nil, fmt.Errorf("delete source: %w", err)
	}

	// Mark target embedding_dirty.
	if _, err := tx.Exec(ctx, `UPDATE books SET embedding_dirty = true WHERE id = $1`, targetID); err != nil {
		return 0, nil, fmt.Errorf("mark dirty: %w", err)
	}

	// Audit row (copy_events style: we use an admin_patch merge source).
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO copy_events (copy_id, from_status, to_status, source, actor_id)
		 SELECT id, status, status, 'admin_merge', $2 FROM book_copies WHERE book_id = $1`,
		targetID, actorID); err != nil {
		return 0, nil, fmt.Errorf("audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}

	isbns, err := r.ISBNsForBook(ctx, targetID)
	return movedCopies, isbns, err
}

// SectionByCode resolves a section code (codes are unique across the one v1 building).
func (r *AdminRepository) SectionByCode(ctx context.Context, code string) (uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM sections WHERE code = $1 LIMIT 2`, code)
	if err != nil {
		return uuid.Nil, fmt.Errorf("section by code: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return uuid.Nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	if len(ids) != 1 {
		return uuid.Nil, model.New(model.CodeNotFound, fmt.Sprintf("Section %q not found.", code))
	}
	return ids[0], nil
}

func mapWriteErr(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "books_isbn13_key":
			return model.New(model.CodeISBNConflict, "A book with this ISBN already exists.")
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "book_isbns_pkey":
			return model.New(model.CodeISBNConflict, "A book with this ISBN already exists.")
		case pgErr.Code == pgUniqueViolation && strings.Contains(pgErr.ConstraintName, "barcode"):
			return model.New(model.CodeConflict, "Barcode is already registered.")
		case pgErr.Code == pgUniqueViolation:
			return model.New(model.CodeConflict, "Resource already exists.")
		case pgErr.Code == pgForeignKeyViolation && strings.Contains(pgErr.ConstraintName, "shelf"):
			return model.New(model.CodeNotFound, "Shelf not found.")
		case pgErr.Code == pgForeignKeyViolation && strings.Contains(pgErr.ConstraintName, "section"):
			return model.New(model.CodeNotFound, "Section not found.")
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func authorsJSON(a []string) ([]byte, error) {
	if a == nil {
		a = []string{}
	}
	return json.Marshal(a)
}

// CreateBook inserts a book and its copies in one transaction, allocating missing
// barcodes and appending one copy_events row per copy.
func (r *AdminRepository) CreateBook(ctx context.Context, actor uuid.UUID, b model.BookFields, copies []model.NewCopy) (uuid.UUID, error) {
	authors, err := authorsJSON(b.Authors)
	if err != nil {
		return uuid.Nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO books (id, title, subtitle, authors, isbn13, isbn10, publisher, published_year, description,
		                   cover_url, open_library_key, dewey_num, loc_num, section_id, embedding_dirty)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,true)`,
		id, b.Title, b.Subtitle, authors, b.ISBN13, b.ISBN10, b.Publisher, b.PublishedYear, b.Description,
		b.CoverURL, b.OpenLibraryKey, b.DeweyNum, b.LOCNum, b.SectionID); err != nil {
		return uuid.Nil, mapWriteErr(err, "insert book")
	}
	// Keep book_isbns in sync: insert the primary ISBN row when isbn13 is provided.
	if b.ISBN13 != nil && *b.ISBN13 != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ($1, $2, true)
			 ON CONFLICT (isbn13) DO NOTHING`,
			*b.ISBN13, id); err != nil {
			return uuid.Nil, mapWriteErr(err, "insert book_isbn")
		}
	}
	if len(copies) > 0 {
		if err := lockAccession(ctx, tx); err != nil {
			return uuid.Nil, err
		}
		for _, c := range copies {
			if _, err := insertCopy(ctx, tx, actor, id, c); err != nil {
				return uuid.Nil, err
			}
		}
	}
	return id, tx.Commit(ctx)
}

// PatchBook applies a partial update; NOT_FOUND for unknown ids. A patch touching a
// RAG field (title/authors/description) marks the book for re-embedding (docs/06 §2).
// When isbn13 changes the book_isbns primary row is kept in sync.
func (r *AdminRepository) PatchBook(ctx context.Context, id uuid.UUID, p model.BookPatch) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sets []string
	args := []any{id}
	set := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	optStr := func(col string, o model.Optional[string]) {
		if o.Set {
			if o.Null {
				set(col, nil)
			} else {
				set(col, o.Value)
			}
		}
	}
	if p.Title != nil {
		set("title", *p.Title)
	}
	if p.Authors != nil {
		a, err := authorsJSON(*p.Authors)
		if err != nil {
			return err
		}
		set("authors", a)
	}
	optStr("subtitle", p.Subtitle)
	optStr("isbn13", p.ISBN13)
	optStr("isbn10", p.ISBN10)
	optStr("publisher", p.Publisher)
	optStr("description", p.Description)
	optStr("cover_url", p.CoverURL)
	optStr("open_library_key", p.OpenLibraryKey)
	optStr("dewey_num", p.DeweyNum)
	optStr("loc_num", p.LOCNum)
	if p.PublishedYear.Set {
		if p.PublishedYear.Null {
			set("published_year", nil)
		} else {
			set("published_year", p.PublishedYear.Value)
		}
	}
	if p.SectionID.Set {
		if p.SectionID.Null {
			set("section_id", nil)
		} else {
			set("section_id", p.SectionID.Value)
		}
	}
	if len(sets) == 0 {
		sets = append(sets, "updated_at = now()")
	}
	if p.TouchesRAG() {
		sets = append(sets, "embedding_dirty = true")
	}
	tag, err := tx.Exec(ctx, `UPDATE books SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return mapWriteErr(err, "patch book")
	}
	if tag.RowsAffected() == 0 {
		return model.New(model.CodeNotFound, "Book not found.")
	}

	// Sync book_isbns primary row when isbn13 changes.
	if p.ISBN13.Set {
		// Remove the old primary isbn row for this book (there can be only one).
		if _, err := tx.Exec(ctx,
			`DELETE FROM book_isbns WHERE book_id = $1 AND is_primary`, id); err != nil {
			return fmt.Errorf("remove old primary isbn: %w", err)
		}
		if !p.ISBN13.Null && p.ISBN13.Value != "" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ($1, $2, true)
				 ON CONFLICT (isbn13) DO UPDATE SET book_id = $2, is_primary = true`,
				p.ISBN13.Value, id); err != nil {
				return mapWriteErr(err, "sync primary isbn")
			}
		}
	}

	return tx.Commit(ctx)
}

// DeleteBook removes a book; copies, embeddings, library and waitlist rows cascade.
func (r *AdminRepository) DeleteBook(ctx context.Context, id uuid.UUID) error {
	return r.DeleteBooks(ctx, []uuid.UUID{id})
}

type blockedBook struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	ActiveLoans int       `json:"active_loans"`
}

func (r *AdminRepository) DeleteBooks(ctx context.Context, ids []uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT id, title FROM books WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return fmt.Errorf("lock books: %w", err)
	}
	books := make(map[uuid.UUID]blockedBook, len(ids))
	for rows.Next() {
		var b blockedBook
		if err := rows.Scan(&b.ID, &b.Title); err != nil {
			rows.Close()
			return err
		}
		books[b.ID] = b
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(books) != len(ids) {
		return model.New(model.CodeNotFound, "Book not found.")
	}

	rows, err = tx.Query(ctx, `SELECT book_id, status FROM book_copies WHERE book_id = ANY($1) ORDER BY book_id, id FOR UPDATE`, ids)
	if err != nil {
		return fmt.Errorf("lock copies: %w", err)
	}
	blockedIDs := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id uuid.UUID
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return err
		}
		if status == model.CopyCheckedOut || status == model.CopyReservedWaitlist {
			blockedIDs[id] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT c.book_id, count(*) FROM loans l JOIN book_copies c ON c.id = l.copy_id WHERE c.book_id = ANY($1) AND l.returned_at IS NULL GROUP BY c.book_id`, ids)
	if err != nil {
		return fmt.Errorf("count open loans: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			rows.Close()
			return err
		}
		b := books[id]
		b.ActiveLoans = count
		books[id] = b
		blockedIDs[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	blocked := make([]blockedBook, 0, len(blockedIDs))
	for _, id := range ids {
		if blockedIDs[id] {
			blocked = append(blocked, books[id])
		}
	}
	if len(blocked) > 0 {
		return model.New(model.CodeConflict, "Books have active loans, checked-out copies, or reserved copies.").WithExtra(map[string]any{"blocked": blocked})
	}
	if _, err := tx.Exec(ctx, `DELETE FROM books WHERE id = ANY($1)`, ids); err != nil {
		return fmt.Errorf("delete books: %w", err)
	}
	return tx.Commit(ctx)
}

// ---- copies ----------------------------------------------------------------

func lockAccession(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(accessionLockKey)); err != nil {
		return fmt.Errorf("accession lock: %w", err)
	}
	return nil
}

// FormatBarcode renders ATH-{accession:6}-C{n:2} (docs/08 §5).
func FormatBarcode(accession, n int) string { return fmt.Sprintf("ATH-%06d-C%02d", accession, n) }

// allocateBarcode continues the accession counter (docs/08 §5): a book that already has
// copies keeps its accession and takes the next copy number; a book without copies takes
// the next accession after both the highest registered barcode and the catalog position
// of every other book (seeded zero-copy books keep their reserved numbers).
func allocateBarcode(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) (string, error) {
	var acc int
	err := tx.QueryRow(ctx, `SELECT substring(barcode from 5 for 6)::int FROM book_copies
		WHERE book_id = $1 ORDER BY barcode LIMIT 1`, bookID).Scan(&acc)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT GREATEST(
			  COALESCE((SELECT max(substring(barcode from 5 for 6)::int) FROM book_copies), 0),
			  $2 + (SELECT count(*) FROM books WHERE id <> $1)) + 1`, bookID, accessionBase).Scan(&acc)
	}
	if err != nil {
		return "", fmt.Errorf("allocate accession: %w", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(substring(barcode from 13 for 2)::int), 0) + 1
		FROM book_copies WHERE barcode LIKE $1`, fmt.Sprintf("ATH-%06d-C%%", acc)).Scan(&n); err != nil {
		return "", fmt.Errorf("allocate copy number: %w", err)
	}
	if n > maxCopyNumber || acc > 999999 {
		return "", model.New(model.CodeConflict, "No copy numbers left for this accession.")
	}
	return FormatBarcode(acc, n), nil
}

func appendEvent(ctx context.Context, tx pgx.Tx, copyID uuid.UUID, from *string, to string, actor uuid.UUID) error {
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx, `INSERT INTO copy_events (copy_id, from_status, to_status, source, actor_id)
		VALUES ($1,$2,$3,$4,$5)`, copyID, from, to, model.SourceAdminPatch, actorID); err != nil {
		return fmt.Errorf("copy event: %w", err)
	}
	return nil
}

func insertCopy(ctx context.Context, tx pgx.Tx, actor, bookID uuid.UUID, c model.NewCopy) (uuid.UUID, error) {
	barcode := c.Barcode
	if barcode == "" {
		var err error
		if barcode, err = allocateBarcode(ctx, tx, bookID); err != nil {
			return uuid.Nil, err
		}
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO book_copies (id, book_id, barcode, status, condition, shelf_id)
		VALUES ($1,$2,$3,$4,$5,$6)`, id, bookID, barcode, c.Status, c.Condition, c.ShelfID); err != nil {
		return uuid.Nil, mapWriteErr(err, "insert copy")
	}
	return id, appendEvent(ctx, tx, id, nil, c.Status, actor)
}

// AddCopy inserts one copy for an existing book (NOT_FOUND when the book is unknown).
func (r *AdminRepository) AddCopy(ctx context.Context, actor, bookID uuid.UUID, c model.NewCopy) (model.AdminCopy, *model.LocationRow, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.AdminCopy{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM books WHERE id = $1)`, bookID).Scan(&exists); err != nil {
		return model.AdminCopy{}, nil, fmt.Errorf("book exists: %w", err)
	}
	if !exists {
		return model.AdminCopy{}, nil, model.New(model.CodeNotFound, "Book not found.")
	}
	if err := lockAccession(ctx, tx); err != nil {
		return model.AdminCopy{}, nil, err
	}
	id, err := insertCopy(ctx, tx, actor, bookID, c)
	if err != nil {
		return model.AdminCopy{}, nil, err
	}
	cp, loc, err := loadCopy(ctx, tx, id)
	if err != nil {
		return model.AdminCopy{}, nil, err
	}
	return cp, loc, tx.Commit(ctx)
}

// PatchCopy updates status/condition/shelf; a status change stamps last_state_change and
// appends a copy_events row. Moving a copy to reserved_waitlist promotes the oldest waiting
// patron of the book to `ready` with a 48 h window (docs/12 §10).
func (r *AdminRepository) PatchCopy(ctx context.Context, actor, id uuid.UUID, p model.CopyPatch, now time.Time) (model.AdminCopy, *model.LocationRow, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.AdminCopy{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var from string
	var bookID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT status, book_id FROM book_copies WHERE id = $1 FOR UPDATE`, id).Scan(&from, &bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AdminCopy{}, nil, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return model.AdminCopy{}, nil, fmt.Errorf("load copy: %w", err)
	}
	sets := []string{"updated_at = now()"}
	args := []any{id}
	set := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	changed := p.Status != nil && *p.Status != from
	if p.Status != nil {
		set("status", *p.Status)
	}
	if changed {
		set("last_state_change", now)
	}
	if p.Condition != nil {
		set("condition", *p.Condition)
	}
	if p.ShelfID.Set {
		if p.ShelfID.Null {
			set("shelf_id", nil)
		} else {
			set("shelf_id", p.ShelfID.Value)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE book_copies SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...); err != nil {
		return model.AdminCopy{}, nil, mapWriteErr(err, "patch copy")
	}
	if changed {
		if err := appendEvent(ctx, tx, id, &from, *p.Status, actor); err != nil {
			return model.AdminCopy{}, nil, err
		}
		if *p.Status == model.CopyReservedWaitlist {
			if _, err := tx.Exec(ctx, `
				UPDATE waitlist SET status = 'ready', ready_at = $2, expires_at = $3
				WHERE id = (SELECT id FROM waitlist WHERE book_id = $1 AND status = 'waiting'
				            ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)`,
				bookID, now, now.Add(ReadyHold)); err != nil {
				return model.AdminCopy{}, nil, fmt.Errorf("promote waitlist: %w", err)
			}
		}
	}
	cp, loc, err := loadCopy(ctx, tx, id)
	if err != nil {
		return model.AdminCopy{}, nil, err
	}
	return cp, loc, tx.Commit(ctx)
}

// CopyIDByBarcode resolves a registered barcode; NOT_FOUND when unknown.
func (r *AdminRepository) CopyIDByBarcode(ctx context.Context, barcode string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = $1`, barcode).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("copy by barcode: %w", err)
	}
	return id, nil
}

// DeleteCopy hard-deletes a copy.
func (r *AdminRepository) DeleteCopy(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM book_copies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete copy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.New(model.CodeNotFound, "Copy not found.")
	}
	return nil
}

func loadCopy(ctx context.Context, db DBTX, id uuid.UUID) (model.AdminCopy, *model.LocationRow, error) {
	var c model.AdminCopy
	var path, bName, fName, sName, aisle, shelf, tier, bCode, sCode *string
	var floorN, rowLevel *int
	err := db.QueryRow(ctx, `
		SELECT bc.id, bc.book_id, bc.barcode, bc.status, bc.condition, bc.shelf_id, bc.last_state_change,
		       bc.created_at, bc.updated_at,
		       bl.path, bl.building_name, bl.floor_number, bl.floor_name, bl.section_name,
		       bl.aisle_code, bl.shelf_code, bl.row_level, bl.height_tier, bl.building_code, bl.section_code
		FROM book_copies bc LEFT JOIN book_locations bl ON bl.copy_id = bc.id
		WHERE bc.id = $1`, id).Scan(&c.ID, &c.BookID, &c.Barcode, &c.Status, &c.Condition, &c.ShelfID,
		&c.LastStateChange, &c.CreatedAt, &c.UpdatedAt,
		&path, &bName, &floorN, &fName, &sName, &aisle, &shelf, &rowLevel, &tier, &bCode, &sCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AdminCopy{}, nil, model.New(model.CodeNotFound, "Copy not found.")
	}
	if err != nil {
		return model.AdminCopy{}, nil, fmt.Errorf("load copy: %w", err)
	}
	if path == nil {
		return c, nil, nil
	}
	return c, &model.LocationRow{
		CopyID: c.ID, Barcode: c.Barcode, BuildingCode: deref(bCode), BuildingName: deref(bName),
		FloorNumber: derefInt(floorN), FloorName: deref(fName), SectionCode: deref(sCode), SectionName: deref(sName),
		AisleCode: deref(aisle), ShelfCode: deref(shelf), RowLevel: derefInt(rowLevel), HeightTier: deref(tier), Path: *path,
	}, nil
}

// ---- building ----------------------------------------------------------------

// Tree returns the (single, v1) building with nested floors/sections/aisles/shelves and
// per-shelf occupancy (copies assigned to the shelf over ShelfSlots).
func (r *AdminRepository) Tree(ctx context.Context) (model.TreeBuilding, error) {
	var b model.TreeBuilding
	err := r.pool.QueryRow(ctx, `SELECT id, code, name FROM buildings ORDER BY code LIMIT 1`).Scan(&b.ID, &b.Code, &b.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.TreeBuilding{}, model.New(model.CodeNotFound, "Building not found.")
	}
	if err != nil {
		return model.TreeBuilding{}, fmt.Errorf("building: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT f.id, f.floor_number, f.name, se.id, se.code, se.name, se.color, a.id, a.code,
		       sh.id, sh.code, sh.row_level, sh.height_tier,
		       (SELECT count(*) FROM book_copies c WHERE c.shelf_id = sh.id)
		FROM floors f
		LEFT JOIN sections se ON se.floor_id = f.id
		LEFT JOIN aisles a ON a.section_id = se.id
		LEFT JOIN shelves sh ON sh.aisle_id = a.id
		WHERE f.building_id = $1
		ORDER BY f.floor_number, se.code, a.code, sh.code`, b.ID)
	if err != nil {
		return model.TreeBuilding{}, fmt.Errorf("tree: %w", err)
	}
	defer rows.Close()
	b.Floors = []model.TreeFloor{}
	for rows.Next() {
		var fID uuid.UUID
		var fNum int
		var fName string
		var seID, aID, shID *uuid.UUID
		var seCode, seName, seColor, aCode, shCode, shTier *string
		var shRow *int
		var occupied int
		if err := rows.Scan(&fID, &fNum, &fName, &seID, &seCode, &seName, &seColor, &aID, &aCode,
			&shID, &shCode, &shRow, &shTier, &occupied); err != nil {
			return model.TreeBuilding{}, err
		}
		if n := len(b.Floors); n == 0 || b.Floors[n-1].ID != fID {
			b.Floors = append(b.Floors, model.TreeFloor{ID: fID, FloorNumber: fNum, Name: fName, Sections: []model.TreeSection{}})
		}
		fl := &b.Floors[len(b.Floors)-1]
		if seID == nil {
			continue
		}
		if n := len(fl.Sections); n == 0 || fl.Sections[n-1].ID != *seID {
			fl.Sections = append(fl.Sections, model.TreeSection{ID: *seID, Code: deref(seCode), Name: deref(seName),
				Color: deref(seColor), Aisles: []model.TreeAisle{}})
		}
		se := &fl.Sections[len(fl.Sections)-1]
		if aID == nil {
			continue
		}
		if n := len(se.Aisles); n == 0 || se.Aisles[n-1].ID != *aID {
			se.Aisles = append(se.Aisles, model.TreeAisle{ID: *aID, Code: deref(aCode), Shelves: []model.TreeShelf{}})
		}
		ai := &se.Aisles[len(se.Aisles)-1]
		if shID == nil {
			continue
		}
		ai.Shelves = append(ai.Shelves, model.TreeShelf{
			ID: *shID, Code: deref(shCode), RowLevel: derefInt(shRow), HeightTier: deref(shTier),
			Path:     fmt.Sprintf("%s/%dF/%s/%s/%s", b.Code, fNum, deref(seCode), deref(aCode), deref(shCode)),
			Occupied: occupied, Total: model.ShelfSlots,
		})
	}
	return b, rows.Err()
}

// Shelf loads the manifest inspector for one shelf: the shelf row, its copies and the
// other shelves of the same aisle.
func (r *AdminRepository) Shelf(ctx context.Context, id uuid.UUID) (model.ShelfInspector, error) {
	var s model.ShelfInspector
	var aisleID uuid.UUID
	var bCode string
	err := r.pool.QueryRow(ctx, `
		SELECT sh.id, sh.code, sh.row_level, sh.height_tier, a.id, a.code, se.code, se.name, se.color,
		       f.floor_number, bd.code
		FROM shelves sh
		JOIN aisles a ON a.id = sh.aisle_id
		JOIN sections se ON se.id = a.section_id
		JOIN floors f ON f.id = se.floor_id
		JOIN buildings bd ON bd.id = f.building_id
		WHERE sh.id = $1`, id).Scan(&s.ID, &s.Code, &s.RowLevel, &s.HeightTier, &aisleID, &s.Aisle,
		&s.Section.Code, &s.Section.Name, &s.Section.Color, &s.Floor, &bCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ShelfInspector{}, model.New(model.CodeNotFound, "Shelf not found.")
	}
	if err != nil {
		return model.ShelfInspector{}, fmt.Errorf("shelf: %w", err)
	}
	pathOf := func(shelfCode string) string {
		return fmt.Sprintf("%s/%dF/%s/%s/%s", bCode, s.Floor, s.Section.Code, s.Aisle, shelfCode)
	}
	s.Path = pathOf(s.Code)

	rows, err := r.pool.Query(ctx, `
		SELECT bc.id, bc.barcode, bc.status, b.id, b.title
		FROM book_copies bc JOIN books b ON b.id = bc.book_id
		WHERE bc.shelf_id = $1 ORDER BY bc.barcode`, id)
	if err != nil {
		return model.ShelfInspector{}, fmt.Errorf("shelf copies: %w", err)
	}
	s.Copies = []model.ShelfCopy{}
	for rows.Next() {
		var c model.ShelfCopy
		if err := rows.Scan(&c.ID, &c.Barcode, &c.Status, &c.BookID, &c.Title); err != nil {
			rows.Close()
			return model.ShelfInspector{}, err
		}
		s.Copies = append(s.Copies, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return model.ShelfInspector{}, err
	}
	s.Occupancy = model.Occupancy{Occupied: len(s.Copies), Total: model.ShelfSlots}

	rows, err = r.pool.Query(ctx, `
		SELECT sh.id, sh.code, (SELECT count(*) FROM book_copies c WHERE c.shelf_id = sh.id)
		FROM shelves sh WHERE sh.aisle_id = $1 AND sh.id <> $2 ORDER BY sh.code`, aisleID, id)
	if err != nil {
		return model.ShelfInspector{}, fmt.Errorf("adjacent shelves: %w", err)
	}
	defer rows.Close()
	s.Adjacent = []model.ShelfMarker{}
	for rows.Next() {
		var m model.ShelfMarker
		if err := rows.Scan(&m.ID, &m.Code, &m.Occupancy.Occupied); err != nil {
			return model.ShelfInspector{}, err
		}
		m.Path = pathOf(m.Code)
		m.Occupancy.Total = model.ShelfSlots
		s.Adjacent = append(s.Adjacent, m)
	}
	return s, rows.Err()
}

// ---- users -------------------------------------------------------------------

// Users pages accounts with library/waitlist counts and last login (latest refresh token).
func (r *AdminRepository) Users(ctx context.Context, page, perPage int) ([]model.AdminUser, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.email::text, u.name, u.role, u.created_at,
		       (SELECT count(*) FROM user_library l WHERE l.user_id = u.id),
		       (SELECT count(*) FROM waitlist w WHERE w.user_id = u.id AND w.status IN ('waiting','ready')),
		       (SELECT max(t.created_at) FROM refresh_tokens t WHERE t.user_id = u.id)
		FROM users u ORDER BY u.created_at, u.email LIMIT $1 OFFSET $2`, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("users: %w", err)
	}
	defer rows.Close()
	out := []model.AdminUser{}
	for rows.Next() {
		var u model.AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.LibraryCount, &u.WaitlistCount,
			&u.LastLoginAt); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// SetRole changes a user's role; NOT_FOUND for unknown ids.
func (r *AdminRepository) SetRole(ctx context.Context, id uuid.UUID, role string) (model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `UPDATE users SET role = $2 WHERE id = $1 RETURNING `+userCols, id, role))
}
