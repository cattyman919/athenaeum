package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// ChatRepository implements ai.Repository: conversations + messages (docs/02) and the
// RAG chunk retrieval of docs/06 §2.
type ChatRepository struct{ pool *pgxpool.Pool }

// NewChatRepository wraps a pool.
func NewChatRepository(pool *pgxpool.Pool) *ChatRepository { return &ChatRepository{pool: pool} }

var errConversationNotFound = model.New(model.CodeNotFound, "Conversation not found.")

// CreateConversation inserts a conversation owned by userID.
func (r *ChatRepository) CreateConversation(ctx context.Context, id, userID uuid.UUID, title string) (model.Conversation, error) {
	c := model.Conversation{ID: id, UserID: userID, Title: title}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO conversations (id, user_id, title) VALUES ($1, $2, $3) RETURNING updated_at`,
		id, userID, title).Scan(&c.UpdatedAt)
	if err != nil {
		return model.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return c, nil
}

// Conversation loads one conversation (any owner); NOT_FOUND when absent.
func (r *ChatRepository) Conversation(ctx context.Context, id uuid.UUID) (model.Conversation, error) {
	var c model.Conversation
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, title, updated_at FROM conversations WHERE id = $1`, id).
		Scan(&c.ID, &c.UserID, &c.Title, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Conversation{}, errConversationNotFound
	}
	if err != nil {
		return model.Conversation{}, fmt.Errorf("conversation: %w", err)
	}
	return c, nil
}

// ListConversations returns one page of the user's conversations, newest activity first.
func (r *ChatRepository) ListConversations(ctx context.Context, userID uuid.UUID, limit, offset int) ([]model.Conversation, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count conversations: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, title, updated_at FROM conversations
		WHERE user_id = $1 ORDER BY updated_at DESC, id LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	out := []model.Conversation{}
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// DeleteConversation removes a conversation (messages cascade).
func (r *ChatRepository) DeleteConversation(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errConversationNotFound
	}
	return nil
}

// SetTitle renames a conversation.
func (r *ChatRepository) SetTitle(ctx context.Context, id uuid.UUID, title string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE conversations SET title = $2 WHERE id = $1`, id, title); err != nil {
		return fmt.Errorf("set title: %w", err)
	}
	return nil
}

// Messages returns every message of a conversation in insertion order.
func (r *ChatRepository) Messages(ctx context.Context, conversationID uuid.UUID) ([]model.ChatMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, role, content, tool_meta, tokens, created_at FROM messages
		WHERE conversation_id = $1 ORDER BY created_at, id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	defer rows.Close()
	out := []model.ChatMessage{}
	for rows.Next() {
		var m model.ChatMessage
		var meta []byte
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &meta, &m.Tokens, &m.CreatedAt); err != nil {
			return nil, err
		}
		if meta != nil {
			m.ToolMeta = meta
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessages inserts messages in one transaction and bumps the conversation's
// updated_at. CreatedAt must be set by the caller (it defines the replay order).
func (r *ChatRepository) AppendMessages(ctx context.Context, conversationID uuid.UUID, msgs []model.ChatMessage) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("append messages: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, m := range msgs {
		var meta any
		if len(m.ToolMeta) > 0 {
			meta = []byte(m.ToolMeta)
		}
		createdAt := m.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO messages (id, conversation_id, role, content, tool_meta, tokens, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			m.ID, conversationID, m.Role, m.Content, meta, m.Tokens, createdAt); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1`, conversationID); err != nil {
		return fmt.Errorf("touch conversation: %w", err)
	}
	return tx.Commit(ctx)
}

// RetrieveChunks returns the top-k description chunks by cosine similarity with at most
// perBook chunks per book and score >= minScore (docs/06 §2), each with the book's home
// shelf path.
func (r *ChatRepository) RetrieveChunks(ctx context.Context, vec []float32, k, perBook int, minScore float64) ([]model.RAGChunk, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.book_id, t.title, t.content, loc.path, t.score FROM (
		  SELECT e.book_id, b.title, e.content, (1 - (e.embedding <=> $1::vector))::float8 AS score,
		         row_number() OVER (PARTITION BY e.book_id ORDER BY e.embedding <=> $1::vector) AS rn
		  FROM (SELECT book_id, content, embedding FROM book_embeddings
		        ORDER BY embedding <=> $1::vector LIMIT 64) e
		  JOIN books b ON b.id = e.book_id) t
		LEFT JOIN LATERAL (
		  SELECT path FROM book_locations
		  WHERE book_id = t.book_id AND path IS NOT NULL
		  ORDER BY (copy_status = 'available') DESC, barcode LIMIT 1) loc ON true
		WHERE t.rn <= $3 AND t.score >= $4
		ORDER BY t.score DESC LIMIT $2`, VectorLiteral(vec), k, perBook, minScore)
	if err != nil {
		return nil, fmt.Errorf("retrieve chunks: %w", err)
	}
	defer rows.Close()
	var out []model.RAGChunk
	for rows.Next() {
		var c model.RAGChunk
		if err := rows.Scan(&c.BookID, &c.Title, &c.Content, &c.Path, &c.Score); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
