package ai

import (
	"context"
	"errors"
	"io"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// MaxToolIterations caps agent tool rounds per turn (docs/06 §3).
const MaxToolIterations = 4

// HistoryMessage is one prior user/assistant message fed to the agent.
type HistoryMessage struct {
	Role    string // model.MsgRoleUser | model.MsgRoleAssistant
	Content string
}

// EngineInput is one agent turn.
type EngineInput struct {
	System  string
	History []HistoryMessage
	User    string
}

// EngineResult summarizes a finished turn.
type EngineResult struct {
	TokensIn  int
	TokensOut int
	Tools     []model.ToolMeta
	Hits      []Citation // books touched by tools, first-seen order
}

// Engine runs the agent loop. It calls emit for thinking/tool_call/tool_result events
// and onToken for every raw text delta as soon as the model produces it — including
// text a model writes before a tool call (docs/06 §6). The Eino implementation is
// EinoEngine; tests may use it with a scripted ToolCallingChatModel.
type Engine interface {
	Run(ctx context.Context, userID uuid.UUID, in EngineInput, emit func(Event), onToken func(string)) (EngineResult, error)
}

// --- per-turn state carried through ctx into tools and the model wrapper ---

type turnKey struct{}

type turnState struct {
	userID  uuid.UUID
	mu      sync.Mutex
	emitFn  func(Event)
	tokenFn func(string)
	sent    bool // any text forwarded yet this turn
	thought bool // any thinking forwarded yet this turn
	tools   []model.ToolMeta
	hits    []Citation
	seen    map[uuid.UUID]int
	in      int
	out     int
}

func turnFrom(ctx context.Context) *turnState {
	if st, ok := ctx.Value(turnKey{}).(*turnState); ok {
		return st
	}
	return &turnState{seen: map[uuid.UUID]int{}} // detached (tools invoked outside a turn)
}

func (s *turnState) emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emitFn != nil {
		s.emitFn(e)
	}
}

// roundSeparator starts a later model round on a new paragraph, so text written
// before a tool call ("Let me check the catalog.") does not run into the answer.
const roundSeparator = "\n\n"

// token forwards one model text delta live. firstInRound marks the first non-empty
// delta of a model call; it gets roundSeparator when earlier rounds already sent text.
func (s *turnState) token(text string, firstInRound bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenFn == nil || text == "" {
		return
	}
	if firstInRound && s.sent {
		text = roundSeparator + text
	}
	s.sent = true
	s.tokenFn(text)
}

// think forwards one reasoning delta live as a thinking event, separating model
// rounds like token does.
func (s *turnState) think(text string, firstInRound bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emitFn == nil || text == "" {
		return
	}
	if firstInRound && s.thought {
		text = roundSeparator + text
	}
	s.thought = true
	s.emitFn(Event{Name: EventThinking, Data: ThinkingData{Text: text}})
}

func (s *turnState) addRecord(m model.ToolMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, m)
}

func (s *turnState) addHit(c Citation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.seen[c.BookID]; ok {
		h := &s.hits[i]
		if h.Path == nil {
			h.Path = c.Path
		}
		if h.CoverURL == nil {
			h.CoverURL = c.CoverURL
		}
		if c.Score > h.Score {
			h.Score = c.Score
		}
		return
	}
	s.seen[c.BookID] = len(s.hits)
	s.hits = append(s.hits, c)
}

func (s *turnState) addUsage(u *schema.TokenUsage) {
	if u == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.in += u.PromptTokens
	s.out += u.CompletionTokens
}

// finalAnswerNudge is appended to the forced final call. tool_choice=none alone is
// advisory: Gemini (with or without reasoning) sometimes still emits tool calls, and
// the agent then exceeds its step budget and the turn fails.
const finalAnswerNudge = "The tool budget for this turn is used up. Write your final answer now from the " +
	"tool results above. Do not call any tools."

// cappedModel enforces MaxToolIterations: once the input holds that many tool rounds
// the model is called with tool_choice=forbidden plus finalAnswerNudge, and any tool
// call it still emits is dropped, so that round always ends the turn with an answer.
// It also collects token usage into the turn state.
type cappedModel struct {
	inner einomodel.ToolCallingChatModel
	max   int
}

func (m *cappedModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	inner, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &cappedModel{inner: inner, max: m.max}, nil
}

// prepare returns the call's input and options; final reports the forced last round.
func (m *cappedModel) prepare(input []*schema.Message, opts []einomodel.Option) ([]*schema.Message, []einomodel.Option, bool) {
	rounds := 0
	for _, msg := range input {
		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			rounds++
		}
	}
	if rounds < m.max {
		return input, opts, false
	}
	nudged := make([]*schema.Message, len(input), len(input)+1)
	copy(nudged, input) // never mutate the agent's own message list
	nudged = append(nudged, schema.SystemMessage(finalAnswerNudge))
	return nudged, append(opts, einomodel.WithToolChoice(schema.ToolChoiceForbidden)), true
}

func (m *cappedModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	in, o, final := m.prepare(input, opts)
	out, err := m.inner.Generate(ctx, in, o...)
	if err == nil && out != nil {
		if final {
			out.ToolCalls = nil
		}
		if out.ResponseMeta != nil {
			turnFrom(ctx).addUsage(out.ResponseMeta.Usage)
		}
	}
	return out, err
}

func (m *cappedModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	in, o, final := m.prepare(input, opts)
	sr, err := m.inner.Stream(ctx, in, o...)
	if err != nil {
		return nil, err
	}
	st := turnFrom(ctx)
	firstInRound, firstThoughtInRound := true, true
	// The converter runs once per chunk, as the agent first reads it from OpenRouter
	// (the tool-call checker drives that read). Forwarding text and reasoning here
	// streams them live; the agent's own output only arrives after the checker has
	// drained the round.
	return schema.StreamReaderWithConvert(sr, func(msg *schema.Message) (*schema.Message, error) {
		if msg == nil {
			return msg, nil
		}
		if final {
			msg.ToolCalls = nil // the budget is spent: this round must be the answer
		}
		if msg.ResponseMeta != nil {
			st.addUsage(msg.ResponseMeta.Usage)
		}
		if msg.ReasoningContent != "" {
			st.think(msg.ReasoningContent, firstThoughtInRound)
			firstThoughtInRound = false
		}
		if msg.Content != "" {
			st.token(msg.Content, firstInRound)
			firstInRound = false
		}
		return msg, nil
	}), nil
}

// anyChunkToolCallChecker scans the whole chunk stream for tool calls: Claude emits
// text before tool calls, so the first-chunk default is unreliable (Eino docs). It
// drains each round before the agent continues, which is why text is streamed from
// cappedModel.Stream rather than from the agent's output.
func anyChunkToolCallChecker(_ context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
	defer sr.Close()
	for {
		msg, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if len(msg.ToolCalls) > 0 {
			return true, nil
		}
	}
}

// EinoEngine is the Eino ReAct agent (docs/06 §3) over the four catalog tools.
type EinoEngine struct {
	agent *react.Agent
}

// NewEinoEngine compiles the agent graph once; per-turn state travels in ctx.
func NewEinoEngine(ctx context.Context, chat einomodel.ToolCallingChatModel, cat Catalog) (*EinoEngine, error) {
	tools, err := newTools(cat, toolTimeout)
	if err != nil {
		return nil, err
	}
	a, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel:      &cappedModel{inner: chat, max: MaxToolIterations},
		ToolsConfig:           compose.ToolsNodeConfig{Tools: tools, UnknownToolsHandler: unknownTool},
		MaxStep:               2*(MaxToolIterations+1) + 2,
		StreamToolCallChecker: anyChunkToolCallChecker,
	})
	if err != nil {
		return nil, err
	}
	return &EinoEngine{agent: a}, nil
}

func unknownTool(_ context.Context, name, _ string) (string, error) {
	return `{"error":"unknown tool ` + name + `"}`, nil
}

// Run executes one turn. Text reaches onToken live from cappedModel.Stream; the
// agent's output stream is only drained here for completion and errors.
func (e *EinoEngine) Run(ctx context.Context, userID uuid.UUID, in EngineInput, emit func(Event), onToken func(string)) (EngineResult, error) {
	st := &turnState{userID: userID, emitFn: emit, tokenFn: onToken, seen: map[uuid.UUID]int{}}
	ctx = context.WithValue(ctx, turnKey{}, st)

	msgs := make([]*schema.Message, 0, len(in.History)+2)
	msgs = append(msgs, schema.SystemMessage(in.System))
	for _, h := range in.History {
		if h.Role == model.MsgRoleAssistant {
			msgs = append(msgs, schema.AssistantMessage(h.Content, nil))
		} else {
			msgs = append(msgs, schema.UserMessage(h.Content))
		}
	}
	msgs = append(msgs, schema.UserMessage(in.User))

	sr, err := e.agent.Stream(ctx, msgs)
	if err != nil {
		return EngineResult{}, asUpstream(err)
	}
	defer sr.Close()
	for {
		_, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return EngineResult{}, asUpstream(err)
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return EngineResult{TokensIn: st.in, TokensOut: st.out, Tools: st.tools, Hits: st.hits}, nil
}

// asUpstream keeps domain errors (e.g. from the ACL adapter) and maps anything else
// from the agent graph to UPSTREAM_ERROR.
func asUpstream(err error) error {
	if _, ok := model.AsError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return model.Wrap(model.CodeUpstreamError, "The AI librarian could not complete this answer.", err)
}
