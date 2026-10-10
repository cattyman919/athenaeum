package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// --- in-memory repository ---

type memRepo struct {
	mu     sync.Mutex
	convs  map[uuid.UUID]model.Conversation
	msgs   map[uuid.UUID][]model.ChatMessage
	chunks []model.RAGChunk
}

func newMemRepo() *memRepo {
	return &memRepo{convs: map[uuid.UUID]model.Conversation{}, msgs: map[uuid.UUID][]model.ChatMessage{}}
}

func (r *memRepo) RetrieveChunks(context.Context, []float32, int, int, float64) ([]model.RAGChunk, error) {
	return r.chunks, nil
}

func (r *memRepo) CreateConversation(_ context.Context, id, userID uuid.UUID, title string) (model.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := model.Conversation{ID: id, UserID: userID, Title: title, UpdatedAt: time.Now()}
	r.convs[id] = c
	return c, nil
}

func (r *memRepo) Conversation(_ context.Context, id uuid.UUID) (model.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.convs[id]
	if !ok {
		return c, model.New(model.CodeNotFound, "Conversation not found.")
	}
	return c, nil
}

func (r *memRepo) ListConversations(_ context.Context, userID uuid.UUID, _, _ int) ([]model.Conversation, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.Conversation{}
	for _, c := range r.convs {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, len(out), nil
}

func (r *memRepo) DeleteConversation(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.convs, id)
	delete(r.msgs, id)
	return nil
}

func (r *memRepo) SetTitle(_ context.Context, id uuid.UUID, title string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.convs[id]
	c.Title = title
	r.convs[id] = c
	return nil
}

func (r *memRepo) Messages(_ context.Context, id uuid.UUID) ([]model.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.ChatMessage(nil), r.msgs[id]...), nil
}

func (r *memRepo) AppendMessages(_ context.Context, id uuid.UUID, msgs []model.ChatMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs[id] = append(r.msgs[id], msgs...)
	return nil
}

// --- embedder / catalog ---

type fakeEmb struct{ err error }

func (e fakeEmb) EmbedQuery(context.Context, string) ([]float32, error) {
	return []float32{1}, e.err
}

var (
	duneID = uuid.MustParse("8c140000-0000-4000-8000-000000000001")
	hypID  = uuid.MustParse("b3aa0000-0000-4000-8000-000000000002")
)

type fakeCatalog struct {
	block    bool
	searches []catalog.Request
	mu       sync.Mutex
}

func (c *fakeCatalog) Search(ctx context.Context, req catalog.Request) (model.SearchPage, error) {
	c.mu.Lock()
	c.searches = append(c.searches, req)
	c.mu.Unlock()
	if c.block {
		<-ctx.Done()
		return model.SearchPage{}, ctx.Err()
	}
	if req.Mode != model.ModeKeyword {
		return model.SearchPage{}, model.ErrEmbeddingUnavail
	}
	return model.SearchPage{Items: []model.BookSummary{{
		ID: duneID, Title: "Dune", Authors: []string{"Frank Herbert"},
		Location:     &model.LocationRef{Path: "MAIN/3F/SF/A-1/S-2", ShelfCode: "S-2"},
		Availability: model.Availability{State: "available"}, Score: 0.9,
	}}}, nil
}

func (c *fakeCatalog) Detail(_ context.Context, id string, _ uuid.UUID) (model.BookDetail, error) {
	if id != duneID.String() {
		return model.BookDetail{}, model.New(model.CodeNotFound, "Book not found.")
	}
	return model.BookDetail{ID: duneID, Title: "Dune",
		Availability: model.DetailAvailability{TotalCopies: 3, AvailableCopies: 2, State: "available"},
		Wayfinding:   &model.Wayfinding{Path: "MAIN/3F/SF/A-1/S-2", LadderSteps: []string{"Floor 3 — North Stacks"}},
		Neighbors:    []model.Neighbor{{ID: hypID, Title: "Hyperion", Authors: []string{"Dan Simmons"}}},
	}, nil
}

// --- scripted Eino chat model ("fake Eino stream", docs/09 §8) ---

type step struct {
	tool   *schema.ToolCall // emit this tool call (split across chunks)
	tokens []string         // or stream these deltas
	err    error            // then fail mid-stream
}

type scriptedModel struct {
	mu       sync.Mutex
	steps    []step
	inputs   [][]*schema.Message
	choices  []schema.ToolChoice
	repeat   bool // keep replaying the last step (tool-loop tests)
	toolless step // used when tool_choice=forbidden
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *scriptedModel) Generate(ctx context.Context, in []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	sr, err := m.Stream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	var ch []*schema.Message
	for {
		c, err := sr.Recv()
		if err != nil {
			break
		}
		ch = append(ch, c)
	}
	return schema.ConcatMessages(ch)
}

func (m *scriptedModel) Stream(_ context.Context, in []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	o := einomodel.GetCommonOptions(nil, opts...)
	m.mu.Lock()
	m.inputs = append(m.inputs, in)
	var choice schema.ToolChoice
	if o.ToolChoice != nil {
		choice = *o.ToolChoice
	}
	m.choices = append(m.choices, choice)
	var s step
	switch {
	case choice == schema.ToolChoiceForbidden:
		s = m.toolless
	case len(m.steps) == 0:
		m.mu.Unlock()
		return nil, errors.New("script exhausted")
	default:
		s = m.steps[0]
		if !m.repeat || len(m.steps) > 1 {
			m.steps = m.steps[1:]
		}
	}
	m.mu.Unlock()

	sr, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer sw.Close()
		if s.tool != nil {
			idx := 0
			sw.Send(&schema.Message{Role: schema.Assistant, Content: "Let me check the catalog."}, nil)
			sw.Send(&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &idx, ID: s.tool.ID,
				Type: "function", Function: schema.FunctionCall{Name: s.tool.Function.Name}}}}, nil)
			sw.Send(&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &idx,
				Function: schema.FunctionCall{Arguments: s.tool.Function.Arguments}}}}, nil)
		}
		for _, t := range s.tokens {
			sw.Send(&schema.Message{Role: schema.Assistant, Content: t}, nil)
		}
		if s.err != nil {
			sw.Send(nil, s.err)
			return
		}
		sw.Send(&schema.Message{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{
			FinishReason: "stop", Usage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 10}}}, nil)
	}()
	return sr, nil
}

func toolStep(id, name, args string) step {
	return step{tool: &schema.ToolCall{ID: id, Function: schema.FunctionCall{Name: name, Arguments: args}}}
}

// --- sink ---

type recSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *recSink) Send(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *recSink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	for i, e := range s.events {
		out[i] = e.Name
	}
	return out
}

func newTestService(t *testing.T, m *scriptedModel, cat *fakeCatalog, repo *memRepo, emb Embedder) *Service {
	t.Helper()
	eng, err := NewEinoEngine(context.Background(), m, cat)
	if err != nil {
		t.Fatal(err)
	}
	return New(repo, emb, eng, Config{Enabled: true, Model: "anthropic/claude-3.5-sonnet"}, logging.Discard())
}
