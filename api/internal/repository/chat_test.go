package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
)

func TestChatRepository(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewChatRepository(pool)
	u := newUser(t, pool, "chat@athenaeum.dev")

	c, err := repo.CreateConversation(ctx, uuid.New(), u, model.DefaultConversationTitle)
	if err != nil || c.UpdatedAt.IsZero() {
		t.Fatalf("create %+v %v", c, err)
	}
	if err := repo.SetTitle(ctx, c.ID, "Babylonian math…"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Conversation(ctx, c.ID)
	if err != nil || got.UserID != u || got.Title != "Babylonian math…" {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := repo.Conversation(ctx, uuid.New()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown %v", err)
	}

	now := time.Now()
	q, a, r := "where?", "here", `{"books":[]}`
	tokens := 12
	if err := repo.AppendMessages(ctx, c.ID, []model.ChatMessage{
		{ID: uuid.New(), Role: model.MsgRoleUser, Content: &q, CreatedAt: now},
		{ID: uuid.New(), Role: model.MsgRoleTool, Content: &r, ToolMeta: []byte(`{"tool_call_id":"call_1","name":"search_catalog","args":{},"result":"{}"}`), CreatedAt: now.Add(time.Millisecond)},
		{ID: uuid.New(), Role: model.MsgRoleAssistant, Content: &a, Tokens: &tokens, CreatedAt: now.Add(2 * time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := repo.Messages(ctx, c.ID)
	if err != nil || len(msgs) != 3 || msgs[0].Role != "user" || msgs[1].ToolMeta == nil || msgs[0].ToolMeta != nil || *msgs[2].Tokens != 12 {
		t.Fatalf("messages %+v %v", msgs, err)
	}
	// An assistant row without stored citations still reads back as an empty list.
	if msgs[2].Citations == nil || len(msgs[2].Citations) != 0 || msgs[2].ToolMeta != nil {
		t.Fatalf("assistant without citations %+v", msgs[2])
	}

	// Final citation cards persist in the assistant row's tool_meta and round-trip.
	path, cover := "MAIN/3F/SF/A-1/S-2", "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg"
	cited := []model.ChatCitation{
		{BookID: uuid.New(), Title: "Dune", Path: &path, Snippet: "Desert planet.", Score: 0.86, CoverURL: &cover},
		{BookID: uuid.New(), Title: "Hyperion"},
	}
	a2 := "Try Dune."
	if err := repo.AppendMessages(ctx, c.ID, []model.ChatMessage{
		{ID: uuid.New(), Role: model.MsgRoleAssistant, Content: &a2, Citations: cited, CreatedAt: now.Add(3 * time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := pool.QueryRow(ctx, `SELECT tool_meta::text FROM messages WHERE conversation_id = $1 AND content = $2`, c.ID, a2).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored model.AssistantMeta
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || len(stored.Citations) != 2 {
		t.Fatalf("stored tool_meta %s %v", raw, err)
	}
	msgs, err = repo.Messages(ctx, c.ID)
	if err != nil || len(msgs) != 4 {
		t.Fatalf("messages %+v %v", msgs, err)
	}
	got4 := msgs[3]
	if got4.ToolMeta != nil || len(got4.Citations) != 2 || got4.Citations[0].BookID != cited[0].BookID ||
		*got4.Citations[0].Path != path || got4.Citations[0].Score != 0.86 || got4.Citations[0].Snippet != "Desert planet." ||
		got4.Citations[0].CoverURL == nil || *got4.Citations[0].CoverURL != cover ||
		got4.Citations[1].Title != "Hyperion" || got4.Citations[1].CoverURL != nil || got4.Citations[1].Path != nil {
		t.Fatalf("citations round-trip %+v", got4.Citations)
	}
	body, err := json.Marshal(got4)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	_ = json.Unmarshal(body, &wire)
	wc, ok := wire["citations"].([]any)
	if !ok || len(wc) != 2 || wc[0].(map[string]any)["cover_url"] != cover || wire["tool_meta"] != nil {
		t.Fatalf("assistant wire shape %s", body)
	}
	if body, _ := json.Marshal(msgs[0]); strings.Contains(string(body), "citations") {
		t.Fatalf("user rows carry no citations: %s", body)
	}

	list, total, err := repo.ListConversations(ctx, u, 20, 0)
	if err != nil || total != 1 || len(list) != 1 || !list[0].UpdatedAt.After(c.UpdatedAt) {
		t.Fatalf("list %+v %d %v", list, total, err)
	}

	// RAG: the one-hot "Dune" axis scores 1 for Dune and 0 elsewhere (below min_score).
	vec := storedEmbedding(t, pool, "Dune")
	chunks, err := repo.RetrieveChunks(ctx, vec, 8, 4, 0.15)
	if err != nil || len(chunks) == 0 || chunks[0].Title != "Dune" || chunks[0].Path == nil || chunks[0].Score < 0.99 ||
		chunks[0].CoverURL == nil || *chunks[0].CoverURL != "/api/books/"+chunks[0].BookID.String()+"/cover" {
		t.Fatalf("chunks %+v %v", chunks, err)
	}
	for _, ch := range chunks {
		if ch.Title != "Dune" {
			t.Fatalf("min_score must exclude other books: %+v", ch)
		}
	}

	if err := repo.DeleteConversation(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := repo.Messages(ctx, c.ID); len(msgs) != 0 {
		t.Fatal("messages must cascade")
	}
	if err := repo.DeleteConversation(ctx, c.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("double delete %v", err)
	}
}

// storedEmbedding reads one indexed chunk vector of the titled book.
func storedEmbedding(t *testing.T, pool *pgxpool.Pool, title string) []float32 {
	t.Helper()
	var txt string
	if err := pool.QueryRow(context.Background(), `
		SELECT e.embedding::text FROM book_embeddings e JOIN books b ON b.id = e.book_id
		WHERE b.title = $1 ORDER BY e.chunk_index LIMIT 1`, title).Scan(&txt); err != nil {
		t.Fatal(err)
	}
	var v []float32
	if err := json.Unmarshal([]byte(txt), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
