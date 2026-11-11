// Package ai implements the AI librarian (docs/06): conversation persistence,
// guardrails, RAG retrieval, the Eino agent loop and the SSE event sequence. The
// Eino/OpenRouter specifics stay behind the Engine interface and providers.go.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Guardrail limits (docs/06 §8).
const (
	MessageCap        = 40   // user + assistant messages per conversation
	ContextTokens     = 8000 // history window incl. system prompt and current turn
	MaxMessageRunes   = 4000
	maxTitleRunes     = 200
	titleWords        = 6
	titleRunesNoSpace = 20
	maxCitations      = 8
	defaultPerPage    = 20
	maxPerPage        = 50
)

// Locales (docs/14 §10).
const (
	LocaleEN = "en"
	LocaleZH = "zh-CN"
)

// Repository is the persistence the chat needs (repository.ChatRepository).
type Repository interface {
	ChunkStore
	CreateConversation(ctx context.Context, id, userID uuid.UUID, title string) (model.Conversation, error)
	Conversation(ctx context.Context, id uuid.UUID) (model.Conversation, error)
	ListConversations(ctx context.Context, userID uuid.UUID, limit, offset int) ([]model.Conversation, int, error)
	DeleteConversation(ctx context.Context, id uuid.UUID) error
	SetTitle(ctx context.Context, id uuid.UUID, title string) error
	Messages(ctx context.Context, conversationID uuid.UUID) ([]model.ChatMessage, error)
	AppendMessages(ctx context.Context, conversationID uuid.UUID, msgs []model.ChatMessage) error
}

// Config configures the service.
type Config struct {
	// Enabled is false when OPENROUTER_API_KEY is absent: POST /api/chat → 503 (docs/01 §7).
	Enabled bool
	// Model is OPENROUTER_MODEL, echoed in the meta event.
	Model string
}

// Service is the chat application service.
type Service struct {
	repo   Repository
	emb    Embedder
	engine Engine
	cfg    Config
	log    *slog.Logger
	now    func() time.Time
}

// New builds the service; engine may be nil when chat is disabled.
func New(repo Repository, emb Embedder, engine Engine, cfg Config, log *slog.Logger) *Service {
	if engine == nil {
		cfg.Enabled = false
	}
	return &Service{repo: repo, emb: emb, engine: engine, cfg: cfg, log: log, now: time.Now}
}

// --- conversation CRUD (docs/03 §7) ---

func (s *Service) owned(ctx context.Context, userID uuid.UUID, id string) (model.Conversation, error) {
	cid, err := uuid.Parse(id)
	if err != nil {
		return model.Conversation{}, model.New(model.CodeNotFound, "Conversation not found.")
	}
	c, err := s.repo.Conversation(ctx, cid)
	if err != nil {
		return model.Conversation{}, err
	}
	if c.UserID != userID {
		return model.Conversation{}, model.New(model.CodeForbidden, "This conversation belongs to another user.")
	}
	return c, nil
}

// List returns the caller's conversations, most recently active first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page, perPage int) (model.ConversationPage, error) {
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = defaultPerPage
	}
	if page < 1 || perPage < 1 || perPage > maxPerPage {
		return model.ConversationPage{}, model.New(model.CodeValidationFailed, "page must be >= 1 and per_page between 1 and 50.")
	}
	items, total, err := s.repo.ListConversations(ctx, userID, perPage, (page-1)*perPage)
	if err != nil {
		return model.ConversationPage{}, err
	}
	return model.ConversationPage{Items: items, Meta: model.PageMeta{
		Page: page, PerPage: perPage, Total: total, TotalPages: (total + perPage - 1) / perPage,
	}}, nil
}

// Create starts an empty conversation; a blank title keeps the DDL default.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, title *string) (model.Conversation, error) {
	t := model.DefaultConversationTitle
	if title != nil && strings.TrimSpace(*title) != "" {
		t = strings.TrimSpace(*title)
		if utf8.RuneCountInString(t) > maxTitleRunes {
			return model.Conversation{}, model.New(model.CodeValidationFailed, "title is too long.")
		}
	}
	return s.repo.CreateConversation(ctx, uuid.New(), userID, t)
}

// Get returns a conversation with its messages.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, id string) (model.ConversationDetail, error) {
	c, err := s.owned(ctx, userID, id)
	if err != nil {
		return model.ConversationDetail{}, err
	}
	msgs, err := s.repo.Messages(ctx, c.ID)
	if err != nil {
		return model.ConversationDetail{}, err
	}
	return model.ConversationDetail{ID: c.ID, Title: c.Title, Messages: msgs}, nil
}

// Delete removes a conversation.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, id string) error {
	c, err := s.owned(ctx, userID, id)
	if err != nil {
		return err
	}
	return s.repo.DeleteConversation(ctx, c.ID)
}

// --- streaming turn (docs/03 §7.1, docs/06 §6) ---

// ChatRequest is the POST /api/chat body plus limiter state.
type ChatRequest struct {
	ConversationID string
	Message        string
	Locale         string
	// DailyQuotaExhausted is true when this turn used the last of the 50/day budget;
	// done.finish_reason becomes rate_limited_daily.
	DailyQuotaExhausted bool
}

// NormalizeLocale maps unknown/missing values to en (docs/14 §10).
func NormalizeLocale(l string) string {
	if l == LocaleZH {
		return LocaleZH
	}
	return LocaleEN
}

// Title derives a conversation title from the first user message: first 6 words + "…"
// (docs/06 §7); unspaced scripts (CJK) are cut at 20 runes.
func Title(msg string) string {
	words := strings.Fields(msg)
	if len(words) == 0 {
		return model.DefaultConversationTitle
	}
	if len(words) == 1 {
		r := []rune(words[0])
		if len(r) > titleRunesNoSpace {
			return string(r[:titleRunesNoSpace]) + "…"
		}
		return words[0]
	}
	if len(words) > titleWords {
		return strings.Join(words[:titleWords], " ") + "…"
	}
	return strings.Join(words, " ")
}

// Turn is a validated, persisted user turn ready to stream.
type Turn struct {
	s        *Service
	userID   uuid.UUID
	conv     model.Conversation
	number   int
	locale   string
	message  string
	history  []HistoryMessage
	daily    bool
	assistID uuid.UUID
}

// ConversationID is the (possibly implicitly created) conversation.
func (t *Turn) ConversationID() uuid.UUID { return t.conv.ID }

// Prepare runs every check that must fail before the first SSE byte (JSON envelope):
// chat configured, body valid, conversation ownership, 40-message cap; then creates the
// conversation when absent, persists the user message and sets the title.
func (s *Service) Prepare(ctx context.Context, userID uuid.UUID, req ChatRequest) (*Turn, error) {
	if !s.cfg.Enabled {
		e := model.New(model.CodeUpstreamError, "The AI librarian is not configured.")
		e.HTTPStatus = http.StatusServiceUnavailable
		return nil, e
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, model.New(model.CodeValidationFailed, "message is required.")
	}
	if utf8.RuneCountInString(msg) > MaxMessageRunes {
		return nil, model.New(model.CodeValidationFailed, "message is too long.")
	}

	var conv model.Conversation
	var prior []model.ChatMessage
	var err error
	if req.ConversationID != "" {
		if conv, err = s.owned(ctx, userID, req.ConversationID); err != nil {
			return nil, err
		}
		if prior, err = s.repo.Messages(ctx, conv.ID); err != nil {
			return nil, err
		}
	}

	var history []HistoryMessage
	users := 0
	for _, m := range prior {
		if m.Role != model.MsgRoleUser && m.Role != model.MsgRoleAssistant {
			continue
		}
		if m.Role == model.MsgRoleUser {
			users++
		}
		if m.Content != nil {
			history = append(history, HistoryMessage{Role: m.Role, Content: *m.Content})
		}
	}
	if len(history)+2 > MessageCap {
		return nil, model.New(model.CodeRateLimited,
			"This conversation reached its 40-message limit. Please start a new conversation.").
			WithExtra(map[string]any{"limit": MessageCap})
	}

	if conv.ID == uuid.Nil {
		if conv, err = s.repo.CreateConversation(ctx, uuid.New(), userID, model.DefaultConversationTitle); err != nil {
			return nil, err
		}
	}
	content := msg
	if err := s.repo.AppendMessages(ctx, conv.ID, []model.ChatMessage{{
		ID: uuid.New(), Role: model.MsgRoleUser, Content: &content, CreatedAt: s.now(),
	}}); err != nil {
		return nil, err
	}
	if users == 0 && conv.Title == model.DefaultConversationTitle {
		conv.Title = Title(msg)
		if err := s.repo.SetTitle(ctx, conv.ID, conv.Title); err != nil {
			return nil, err
		}
	}
	return &Turn{
		s: s, userID: userID, conv: conv, number: users + 1, locale: NormalizeLocale(req.Locale),
		message: msg, history: history, daily: req.DailyQuotaExhausted, assistID: uuid.New(),
	}, nil
}

// SystemPrompt renders the rails of docs/06 §8, the locale directive of docs/14 §10
// and the numbered RAG findings.
func SystemPrompt(locale, findings string) string {
	directive := "Reply language: answer in English."
	if locale == LocaleZH {
		directive = "Reply language: answer in Simplified Chinese (zh-CN). Keep book titles exactly as the catalog " +
			"lists them. Still call search_catalog (formulate the search query in English) and still cite only " +
			"catalog shelf paths."
	}
	return "You are the Athenaeum AI Librarian, a research assistant for the Athenaeum library.\n" +
		"Rules:\n" +
		"- Recommend only books that exist in the Athenaeum catalog (use search_catalog); cite only retrieved " +
		"shelf paths; do not invent titles, barcodes, or racks.\n" +
		"- Use get_book_location for shelf paths, check_availability for copy status and recommend_similar " +
		"for related books.\n" +
		"- When you rely on a numbered catalog finding, cite it with its marker, e.g. [doc#1].\n" +
		"- If the question is not about books, reading or this library, politely decline without using tools.\n" +
		"- Keep answers concise.\n" +
		directive + "\n\n" + findings
}

// EstimateTokens approximates tokens: ~4 ASCII chars per token, 1 token per other rune.
func EstimateTokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

// TrimHistory keeps the newest history messages that fit the window together with the
// system prompt and current turn (oldest-first drop; system + current never dropped).
func TrimHistory(system string, history []HistoryMessage, user string, budget int) []HistoryMessage {
	used := EstimateTokens(system) + EstimateTokens(user)
	start := len(history)
	for i := len(history) - 1; i >= 0; i-- {
		n := EstimateTokens(history[i].Content) + 4
		if used+n > budget {
			break
		}
		used += n
		start = i
	}
	return history[start:]
}

// Run streams the turn: meta → (tool_call → tool_result)* → token* → citations → done.
// Any failure emits a single error event and ends the stream (no done).
func (t *Turn) Run(ctx context.Context, sink Sink) {
	s := t.s
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	gone := false
	send := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		if gone {
			return
		}
		if err := sink.Send(e); err != nil {
			gone = true
			cancel()
		}
	}
	isGone := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gone
	}
	fail := func(err error) {
		if isGone() || errors.Is(err, context.Canceled) {
			return
		}
		de, ok := model.AsError(err)
		if !ok {
			de = model.New(model.CodeUnexpected, "An unexpected error occurred.")
		}
		s.log.WarnContext(ctx, "chat turn failed", "conversation_id", t.conv.ID, "code", de.Code, "err", err)
		send(Event{Name: EventError, Data: ErrorData{Code: string(de.Code), Message: de.Message}})
	}

	send(Event{Name: EventMeta, Data: MetaData{ConversationID: t.conv.ID, Turn: t.number, Model: s.cfg.Model, Locale: t.locale}})

	chunks, err := retrieve(ctx, s.emb, s.repo, t.message)
	if err != nil {
		// RAG is best effort: tools still reach the catalog (keyword fallback).
		s.log.WarnContext(ctx, "chat rag retrieval unavailable", "conversation_id", t.conv.ID, "err", err)
		chunks = nil
	}
	system := SystemPrompt(t.locale, findingsBlock(chunks))
	history := TrimHistory(system, t.history, t.message, ContextTokens)

	filter := newMarkerFilter()
	// Thinking is streamed and also kept for the assistant row; thinking_ms is the wait
	// from the start of the turn to the first answer token (docs/06 §6-§7). The engine
	// serializes these callbacks, and both are read only after Run returns.
	started := time.Now()
	var thinking strings.Builder
	var firstToken time.Duration
	res, err := s.engine.Run(ctx, t.userID, EngineInput{System: system, History: history, User: t.message},
		func(e Event) {
			if d, ok := e.Data.(ThinkingData); ok && e.Name == EventThinking {
				thinking.WriteString(d.Text)
			}
			send(e)
		},
		func(delta string) {
			if out := filter.Push(delta); out != "" {
				if firstToken == 0 {
					firstToken = time.Since(started)
				}
				send(Event{Name: EventToken, Data: TokenData{Text: out}})
			}
		})
	if err != nil {
		fail(err)
		return
	}
	if out := filter.Flush(); out != "" {
		send(Event{Name: EventToken, Data: TokenData{Text: out}})
	}
	if isGone() {
		return
	}
	answer := filter.Text()
	citations := BuildCitations(chunks, filter.Cited(), res.Hits, answer)
	send(Event{Name: EventCitations, Data: CitationsData{Citations: citations}})

	if res.TokensIn == 0 {
		in := EstimateTokens(system) + EstimateTokens(t.message)
		for _, h := range history {
			in += EstimateTokens(h.Content)
		}
		res.TokensIn = in
	}
	if res.TokensOut == 0 {
		res.TokensOut = EstimateTokens(answer)
	}

	base := s.now()
	msgs := make([]model.ChatMessage, 0, len(res.Tools)+1)
	for i, tm := range res.Tools {
		meta, _ := json.Marshal(tm)
		content := tm.Result
		msgs = append(msgs, model.ChatMessage{ID: uuid.New(), Role: model.MsgRoleTool, Content: &content,
			ToolMeta: meta, CreatedAt: base.Add(time.Duration(i) * time.Millisecond)})
	}
	tokensOut := res.TokensOut
	assistant := model.ChatMessage{ID: t.assistID, Role: model.MsgRoleAssistant, Content: &answer,
		Tokens: &tokensOut, Citations: citations, CreatedAt: base.Add(time.Duration(len(res.Tools)) * time.Millisecond)}
	if thought := thinking.String(); thought != "" {
		if firstToken == 0 {
			firstToken = time.Since(started) // no answer text: the whole turn was thinking
		}
		ms := int(firstToken.Milliseconds())
		assistant.Thinking, assistant.ThinkingMs = &thought, &ms
	}
	msgs = append(msgs, assistant)
	if err := s.repo.AppendMessages(context.WithoutCancel(ctx), t.conv.ID, msgs); err != nil {
		fail(err)
		return
	}
	s.log.InfoContext(ctx, "chat turn done", "conversation_id", t.conv.ID, "turn", t.number,
		"tools", len(res.Tools), "answer_len", len(answer), "thinking_len", thinking.Len(),
		"tokens_in", res.TokensIn, "tokens_out", res.TokensOut)

	finish := FinishStop
	if t.daily {
		finish = FinishRateLimitedDaily
	}
	send(Event{Name: EventDone, Data: DoneData{MessageID: t.assistID, TokensIn: res.TokensIn,
		TokensOut: res.TokensOut, FinishReason: finish}})
}

// BuildCitations merges the RAG findings referenced by [doc#N] (mention order) with
// tool-hit books whose title appears in the answer, deduplicated by book, max 8.
func BuildCitations(chunks []model.RAGChunk, cited []int, hits []Citation, answer string) []Citation {
	out := []Citation{}
	seen := map[uuid.UUID]bool{}
	for _, n := range cited {
		if n < 1 || n > len(chunks) || len(out) == maxCitations {
			continue
		}
		c := chunks[n-1]
		if seen[c.BookID] {
			continue
		}
		seen[c.BookID] = true
		out = append(out, Citation{BookID: c.BookID, Title: c.Title, Path: c.Path, Snippet: snippet(c.Content), Score: c.Score,
			CoverURL: c.CoverURL})
	}
	lower := strings.ToLower(answer)
	for _, h := range hits {
		if len(out) == maxCitations {
			break
		}
		if seen[h.BookID] || h.Title == "" || !strings.Contains(lower, strings.ToLower(h.Title)) {
			continue
		}
		seen[h.BookID] = true
		out = append(out, h)
	}
	return out
}
