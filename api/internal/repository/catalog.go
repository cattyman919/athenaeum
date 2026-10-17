package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// CatalogRepository implements catalog.Repository.
type CatalogRepository struct{ pool *pgxpool.Pool }

// NewCatalogRepository wraps a pool.
func NewCatalogRepository(pool *pgxpool.Pool) *CatalogRepository {
	return &CatalogRepository{pool: pool}
}

// eraClause maps era buckets to published_year ranges (docs/03 §5.1).
var eraClause = map[string]string{
	"pre1800":   "b.published_year < 1800",
	"1800_1970": "(b.published_year >= 1800 AND b.published_year < 1970)",
	"1970_2000": "(b.published_year >= 1970 AND b.published_year < 2000)",
	"2000_n":    "b.published_year >= 2000",
}

// filterSQL renders facet predicates on alias `b`, appending args and returning
// the clauses (without a leading AND).
func filterSQL(f model.SearchFilters, args []any) ([]string, []any) {
	var clauses []string
	if len(f.Sections) > 0 {
		args = append(args, f.Sections)
		clauses = append(clauses, fmt.Sprintf("b.section_id IN (SELECT id FROM sections WHERE code = ANY($%d))", len(args)))
	}
	if f.AvailableOnly {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM book_copies bc WHERE bc.book_id = b.id AND bc.status = 'available')")
	}
	if len(f.Eras) > 0 {
		var ors []string
		for _, e := range f.Eras {
			if c, ok := eraClause[e]; ok {
				ors = append(ors, c)
			}
		}
		if len(ors) > 0 {
			clauses = append(clauses, "("+strings.Join(ors, " OR ")+")")
		}
	}
	return clauses, args
}

const ftsDoc = `to_tsvector('english', b.title || ' ' || b.title || ' ' || coalesce(b.description, ''))`

// Keyword ranks by FTS rank plus title-trigram, author matches, and ISBN match via
// book_isbns (plan M21: searching an alt-ISBN finds the book).
func (r *CatalogRepository) Keyword(ctx context.Context, f model.SearchFilters, q string) ([]model.SearchHit, error) {
	var args []any
	var where []string
	score := "0::float8"
	order := "b.title"
	if q != "" {
		args = append(args, q)
		match := fmt.Sprintf(`(%[1]s @@ websearch_to_tsquery('english', $1)
			OR b.title %% $1
			OR position(lower($1) in lower(b.title)) > 0
			OR position(lower($1) in lower(b.authors::text)) > 0
			OR EXISTS (SELECT 1 FROM book_isbns bi WHERE bi.book_id = b.id AND bi.isbn13 = $1))`, ftsDoc)
		where = append(where, match)
		score = fmt.Sprintf(`(ts_rank(%s, websearch_to_tsquery('english', $1))
			+ 0.3 * similarity(b.title, $1)
			+ CASE WHEN position(lower($1) in lower(b.title)) > 0 THEN 0.3 ELSE 0 END
			+ CASE WHEN position(lower($1) in lower(b.authors::text)) > 0 THEN 0.2 ELSE 0 END
			+ CASE WHEN EXISTS (SELECT 1 FROM book_isbns bi WHERE bi.book_id = b.id AND bi.isbn13 = $1) THEN 1.0 ELSE 0 END)::float8`, ftsDoc)
		order = "score DESC, b.title"
	}
	fc, args := filterSQL(f, args)
	where = append(where, fc...)
	sql := "SELECT b.id, " + score + " AS score FROM books b"
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY " + order
	return r.hits(ctx, sql, args...)
}

// Semantic ranks books by their best chunk's cosine similarity.
func (r *CatalogRepository) Semantic(ctx context.Context, f model.SearchFilters, vec []float32, limit int) ([]model.SearchHit, error) {
	args := []any{VectorLiteral(vec)}
	fc, args := filterSQL(f, args)
	where := ""
	if len(fc) > 0 {
		where = " WHERE " + strings.Join(fc, " AND ")
	}
	args = append(args, limit)
	sql := fmt.Sprintf(`SELECT book_id, max(score)::float8 AS score FROM (
		SELECT e.book_id, 1 - (e.embedding <=> $1::vector) AS score
		FROM book_embeddings e JOIN books b ON b.id = e.book_id%s
		ORDER BY e.embedding <=> $1::vector LIMIT 50) t
		GROUP BY book_id ORDER BY max(score) DESC LIMIT $%d`, where, len(args))
	return r.hits(ctx, sql, args...)
}

func (r *CatalogRepository) hits(ctx context.Context, sql string, args ...any) ([]model.SearchHit, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()
	var out []model.SearchHit
	for rows.Next() {
		var h model.SearchHit
		if err := rows.Scan(&h.BookID, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Summaries hydrates search results.
func (r *CatalogRepository) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.BookSummary, error) {
	out := make(map[uuid.UUID]model.BookSummary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT b.id, b.title, b.authors, b.isbn13, b.cover_url, b.published_year,
		       se.code, se.name, se.color,
		       coalesce(c.total, 0), coalesce(c.avail, 0), coalesce(c.statuses, '{}'),
		       loc.path, loc.shelf_code
		FROM books b
		LEFT JOIN sections se ON se.id = b.section_id
		LEFT JOIN LATERAL (
		  SELECT count(*)::int AS total,
		         (count(*) FILTER (WHERE status = 'available'))::int AS avail,
		         array_agg(DISTINCT status) AS statuses
		  FROM book_copies WHERE book_id = b.id) c ON true
		LEFT JOIN LATERAL (
		  SELECT path, shelf_code FROM book_locations
		  WHERE book_id = b.id AND path IS NOT NULL
		  ORDER BY (copy_status = 'available') DESC, barcode LIMIT 1) loc ON true
		WHERE b.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b model.BookSummary
		var authors []byte
		var secCode, secName, secColor, path, shelf *string
		if err := rows.Scan(&b.ID, &b.Title, &authors, &b.ISBN13, &b.CoverURL, &b.PublishedYear,
			&secCode, &secName, &secColor,
			&b.Availability.TotalCopies, &b.Availability.AvailableCopies, &b.Statuses, &path, &shelf); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(authors, &b.Authors); err != nil {
			return nil, fmt.Errorf("authors of %s: %w", b.ID, err)
		}
		if b.Authors == nil {
			b.Authors = []string{}
		}
		if secCode != nil {
			b.Section = &model.SectionRef{Code: *secCode, Name: deref(secName), Color: deref(secColor)}
		}
		if path != nil {
			b.Location = &model.LocationRef{Path: *path, ShelfCode: deref(shelf)}
		}
		out[b.ID] = b
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
