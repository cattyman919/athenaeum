package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Message roles (docs/02 conversations / messages).
const (
	MsgRoleSystem    = "system"
	MsgRoleUser      = "user"
	MsgRoleAssistant = "assistant"
	MsgRoleTool      = "tool"
)

// DefaultConversationTitle is the DDL default title (docs/02).
const DefaultConversationTitle = "New conversation"

// Conversation is one chat thread (GET /api/chat/conversations item, docs/03 §7).
type Conversation struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	// UserID is the owner (ownership checks only; not serialized).
	UserID uuid.UUID `json:"-"`
}

// ChatMessage is a persisted message row (docs/03 §7 GET /api/chat/conversations/{id}).
type ChatMessage struct {
	ID        uuid.UUID       `json:"id"`
	Role      string          `json:"role"`
	Content   *string         `json:"content"`
	ToolMeta  json.RawMessage `json:"tool_meta"`
	CreatedAt time.Time       `json:"created_at"`
	// Tokens is the stored token count (not part of the API payload).
	Tokens *int `json:"-"`
	// Citations are the final citation cards of an assistant row, persisted in
	// tool_meta as {"citations":[…]} (docs/06 §7) and serialized only on assistant rows.
	Citations []ChatCitation `json:"-"`
}

// MarshalJSON adds `citations` (never null) to assistant rows (docs/03 §7).
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type plain ChatMessage
	if m.Role != MsgRoleAssistant {
		return json.Marshal(plain(m))
	}
	cits := m.Citations
	if cits == nil {
		cits = []ChatCitation{}
	}
	return json.Marshal(struct {
		plain
		Citations []ChatCitation `json:"citations"`
	}{plain(m), cits})
}

// ChatCitation is one citation card: the `citations` SSE entry (docs/06 §6) and the
// persisted `citations` of an assistant message.
type ChatCitation struct {
	BookID   uuid.UUID `json:"book_id"`
	Title    string    `json:"title"`
	Path     *string   `json:"path"`
	Snippet  string    `json:"snippet"`
	Score    float64   `json:"score"`
	CoverURL *string   `json:"cover_url"`
}

// AssistantMeta is the messages.tool_meta JSONB for assistant rows: {citations}.
type AssistantMeta struct {
	Citations []ChatCitation `json:"citations"`
}

// ToolMeta is the messages.tool_meta JSONB for tool rows: {tool_call_id, name, args, result}.
type ToolMeta struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
	Result     string          `json:"result"`
}

// ConversationDetail is GET /api/chat/conversations/{id}.
type ConversationDetail struct {
	ID       uuid.UUID     `json:"id"`
	Title    string        `json:"title"`
	Messages []ChatMessage `json:"messages"`
}

// ConversationPage is GET /api/chat/conversations.
type ConversationPage struct {
	Items []Conversation `json:"items"`
	Meta  PageMeta       `json:"meta"`
}

// RAGChunk is one retrieved description chunk (docs/06 §2 query side).
type RAGChunk struct {
	BookID   uuid.UUID
	Title    string
	Content  string
	Path     *string // home shelf; nil when the book has no shelved copy
	Score    float64
	CoverURL *string // books.cover_url
}
