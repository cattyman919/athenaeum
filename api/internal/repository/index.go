package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

// Replace rewrites embeddings for the given books in one transaction.
func (s *IndexStore) Replace(ctx context.Context, bookIDs []string, rows []model.ChunkRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
		maxIdx[id] = -1
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
