package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// IndexStore implements index.Store.
type IndexStore struct{ pool *pgxpool.Pool }

// NewIndexStore wraps a pool.
func NewIndexStore(pool *pgxpool.Pool) *IndexStore { return &IndexStore{pool: pool} }

// VectorLiteral renders a pgvector text literal such as "[0.1,0.2]".
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// Books returns books to index, ordered by creation then title for determinism.
func (s *IndexStore) Books(ctx context.Context, bookID string) ([]model.IndexBook, error) {
	q := `SELECT id::text, title, authors, coalesce(description,'') FROM books`
	var args []any
	if bookID != "" {
		q += ` WHERE id::text = $1`
		args = append(args, bookID)
	}
	q += ` ORDER BY created_at, title`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()
	var out []model.IndexBook
	for rows.Next() {
		var b model.IndexBook
		var authors []byte
		if err := rows.Scan(&b.ID, &b.Title, &authors, &b.Description); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(authors, &b.Authors); err != nil {
			return nil, fmt.Errorf("authors of %s: %w", b.ID, err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Replace rewrites embeddings for the given books in one transaction. Books deleted
// since they were read are skipped (the delete cascades their rows anyway).
func (s *IndexStore) Replace(ctx context.Context, bookIDs []string, rows []model.ChunkRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	live := map[string]bool{}
	if len(bookIDs) > 0 {
		idRows, err := tx.Query(ctx, `SELECT id::text FROM books WHERE id::text = ANY($1) ORDER BY id FOR KEY SHARE`, bookIDs)
		if err != nil {
			return fmt.Errorf("lock books: %w", err)
		}
		for idRows.Next() {
			var id string
			if err := idRows.Scan(&id); err != nil {
				idRows.Close()
				return err
			}
			live[id] = true
		}
		idRows.Close()
		if err := idRows.Err(); err != nil {
			return err
		}
	}
	kept := rows[:0:0]
	for _, r := range rows {
		if live[r.BookID] {
			kept = append(kept, r)
		}
	}
	rows = kept

	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(
			`INSERT INTO book_embeddings (book_id, chunk_index, content, embedding, model, chunker_version)
			 VALUES ($1::uuid, $2, $3, $4::vector, $5, $6)
			 ON CONFLICT (book_id, chunk_index) DO UPDATE
			 SET content = EXCLUDED.content, embedding = EXCLUDED.embedding,
			     model = EXCLUDED.model, chunker_version = EXCLUDED.chunker_version`,
			r.BookID, r.ChunkIndex, r.Content, VectorLiteral(r.Embedding), r.Model, r.ChunkerVersion)
	}
	br := tx.SendBatch(ctx, batch)
	for range rows {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("upsert chunk: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return err
	}

	// Drop leftover chunk indexes (e.g. description got shorter).
	maxIdx := map[string]int{}
	for _, id := range bookIDs {
		if live[id] {
			maxIdx[id] = -1
		}
	}
	for _, r := range rows {
		if r.ChunkIndex > maxIdx[r.BookID] {
			maxIdx[r.BookID] = r.ChunkIndex
		}
	}
	for id, m := range maxIdx {
		if _, err := tx.Exec(ctx, `DELETE FROM book_embeddings WHERE book_id = $1::uuid AND chunk_index > $2`, id, m); err != nil {
			return fmt.Errorf("prune chunks: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// StaleBooks lists books with rows from another model/chunker version.
func (s *IndexStore) StaleBooks(ctx context.Context, modelName, chunkerVersion string) ([]model.StaleBook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT b.id::text, b.title, count(*)
		 FROM book_embeddings e JOIN books b ON b.id = e.book_id
		 WHERE e.model <> $1 OR e.chunker_version <> $2
		 GROUP BY b.id, b.title ORDER BY b.title`, modelName, chunkerVersion)
	if err != nil {
		return nil, fmt.Errorf("stale books: %w", err)
	}
	defer rows.Close()
	var out []model.StaleBook
	for rows.Next() {
		var sb model.StaleBook
		if err := rows.Scan(&sb.ID, &sb.Title, &sb.Chunks); err != nil {
			return nil, err
		}
		out = append(out, sb)
	}
	return out, rows.Err()
}

// StaleChunkCount counts rows from another model/chunker version.
func (s *IndexStore) StaleChunkCount(ctx context.Context, modelName, chunkerVersion string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM book_embeddings WHERE model <> $1 OR chunker_version <> $2`,
		modelName, chunkerVersion).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("stale count: %w", err)
	}
	return n, nil
}

// pendingWhere / pendingOrder define the auto-index backlog shared by the worker
// (PendingBooks) and the visibility endpoints (PendingStatus, PendingCount).
const (
	pendingWhere = `(b.embedding_dirty
		   OR (coalesce(b.description, '') ~ '\S'
		       AND NOT EXISTS (SELECT 1 FROM book_embeddings e WHERE e.book_id = b.id)))`
	pendingOrder = `b.embedding_dirty DESC, b.updated_at, b.id`
)

// PendingBooks lists the auto-index backlog (docs/06 §2): books flagged
// embedding_dirty plus books with a non-blank description and no embedding rows
// (fresh-seed backfill). Blank-description books never embed, so they only appear
// while flagged. Flagged books come first, then oldest edits.
func (s *IndexStore) PendingBooks(ctx context.Context) ([]model.PendingBook, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id::text, b.embedding_dirty, b.updated_at FROM books b
		WHERE `+pendingWhere+`
		ORDER BY `+pendingOrder)
	if err != nil {
		return nil, fmt.Errorf("pending books: %w", err)
	}
	defer rows.Close()
	var out []model.PendingBook
	for rows.Next() {
		var p model.PendingBook
		if err := rows.Scan(&p.ID, &p.Dirty, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ClearDirty resets embedding_dirty only when the book is still flagged and
// unchanged since the snapshot, so an edit racing the embed keeps it queued.
func (s *IndexStore) ClearDirty(ctx context.Context, bookID string, snapshot time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE books SET embedding_dirty = false
		 WHERE id = $1::uuid AND embedding_dirty AND updated_at = $2`, bookID, snapshot)
	if err != nil {
		return false, fmt.Errorf("clear dirty: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// PendingCount counts the auto-index backlog (same set as PendingBooks).
func (s *IndexStore) PendingCount(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM books b WHERE `+pendingWhere).Scan(&n); err != nil {
		return 0, fmt.Errorf("pending count: %w", err)
	}
	return n, nil
}

// PendingStatus returns the backlog size plus up to limit pending ids in worker
// order (same set as PendingBooks).
func (s *IndexStore) PendingStatus(ctx context.Context, limit int) (model.IndexStatus, error) {
	st := model.IndexStatus{PendingBookIDs: []string{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return st, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM books b WHERE `+pendingWhere).Scan(&st.PendingCount); err != nil {
		return st, fmt.Errorf("pending count: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT b.id::text FROM books b WHERE `+pendingWhere+
		` ORDER BY `+pendingOrder+` LIMIT $1`, limit)
	if err != nil {
		return st, fmt.Errorf("pending ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return st, err
		}
		st.PendingBookIDs = append(st.PendingBookIDs, id)
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	return st, tx.Commit(ctx)
}
