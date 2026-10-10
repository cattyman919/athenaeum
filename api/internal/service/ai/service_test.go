package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

func strp(s string) *string { return &s }

// sequence renders event names compactly: token runs collapse to "token+".
func sequence(names []string) string {
	var out []string
	for i, n := range names {
		if n == EventToken && i > 0 && names[i-1] == EventToken {
			continue
		}
		if n == EventToken && i+1 < len(names) && names[i+1] == EventToken {
			out = append(out, "token+")
			continue
		}
		out = append(out, n)
	}
	return strings.Join(out, ",")
}

func TestTurnSSEOrderAndPersistence(t *testing.T) {
	repo := newMemRepo()
	repo.chunks = []model.RAGChunk{{BookID: duneID, Title: "Dune", Content: "Desert planet Arrakis and its spice.",
		Path: strp("MAIN/3F/SF/A-1/S-2"), Score: 0.86, CoverURL: &duneCover}}
	cat := &fakeCatalog{}
	m := &scriptedModel{steps: []step{
		toolStep("call_1", ToolSearchCatalog, `{"query":"desert planet","mode":"semantic"}`),
		{tokens: []string{"Try Dune ", "[do", "c#1] on ", "shelf S-2."}},
	}}
	svc := newTestService(t, m, cat, repo, fakeEmb{})
	user := uuid.New()

	turn, err := svc.Prepare(context.Background(), user, ChatRequest{Message: "Any novels about a desert planet with spice trade?"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recSink{}
	turn.Run(context.Background(), sink)

	if got, want := sequence(sink.names()), "meta,tool_call,tool_result,token+,citations,done"; got != want {
		t.Fatalf("event order\n got %s\nwant %s", got, want)
	}
	ev := sink.events
	meta := ev[0].Data.(MetaData)
	if meta.Turn != 1 || meta.Locale != LocaleEN || meta.Model != "anthropic/claude-3.5-sonnet" || meta.ConversationID != turn.ConversationID() {
		t.Fatalf("meta %+v", meta)
	}
	call := ev[1].Data.(ToolCallData)
	res := ev[2].Data.(ToolResultData)
	if call.ID != "call_1" || call.Name != ToolSearchCatalog || res.ID != "call_1" || res.Summary != "1 book found" {
		t.Fatalf("tool events %+v %+v", call, res)
	}
	var args map[string]any
	if err := json.Unmarshal(call.Args, &args); err != nil || args["query"] != "desert planet" {
		t.Fatalf("args %s", call.Args)
	}
	// semantic failed (embeddings down) → keyword fallback.
	if len(cat.searches) != 2 || cat.searches[1].Mode != model.ModeKeyword {
		t.Fatalf("searches %+v", cat.searches)
	}
	var text strings.Builder
	for _, e := range ev {
		if e.Name == EventToken {
			text.WriteString(e.Data.(TokenData).Text)
		}
	}
	if text.String() != "Try Dune  on shelf S-2." {
		t.Fatalf("tokens %q (markers must be stripped)", text.String())
	}
	cits := ev[len(ev)-2].Data.(CitationsData).Citations
	if len(cits) != 1 || cits[0].BookID != duneID || *cits[0].Path != "MAIN/3F/SF/A-1/S-2" || cits[0].Score != 0.86 || cits[0].Snippet == "" ||
		cits[0].CoverURL == nil || *cits[0].CoverURL != duneCover {
		t.Fatalf("citations %+v", cits)
	}
	// The SSE payload carries cover_url on the wire.
	wire, _ := json.Marshal(ev[len(ev)-2].Data)
	if !strings.Contains(string(wire), `"cover_url":"`+duneCover+`"`) {
		t.Fatalf("citations wire %s", wire)
	}
	done := ev[len(ev)-1].Data.(DoneData)
	if done.FinishReason != FinishStop || done.TokensIn != 200 || done.TokensOut != 20 || done.MessageID == uuid.Nil {
		t.Fatalf("done %+v", done)
	}

	// The system prompt carries rails, locale directive and numbered findings.
	sys := m.inputs[0][0]
	if sys.Role != schema.System || !strings.Contains(sys.Content, "do not invent titles, barcodes, or racks") ||
		!strings.Contains(sys.Content, "answer in English") || !strings.Contains(sys.Content, `[doc#1] book_id=`+duneID.String()) {
		t.Fatalf("system prompt %q", sys.Content)
	}

	msgs := repo.msgs[turn.ConversationID()]
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[1].Role != "tool" || msgs[2].Role != "assistant" {
		t.Fatalf("persisted %+v", msgs)
	}
	var tm model.ToolMeta
	if err := json.Unmarshal(msgs[1].ToolMeta, &tm); err != nil || tm.ToolCallID != "call_1" || tm.Name != ToolSearchCatalog || tm.Result == "" {
		t.Fatalf("tool_meta %s", msgs[1].ToolMeta)
	}
	if *msgs[2].Content != "Try Dune  on shelf S-2." || msgs[2].ID != done.MessageID {
		t.Fatalf("assistant %+v", msgs[2])
	}
	// The streamed citation cards persist on the assistant message (docs/06 §7).
	if len(msgs[2].Citations) != 1 || msgs[2].Citations[0].BookID != duneID || msgs[2].Citations[0].CoverURL == nil {
		t.Fatalf("persisted citations %+v", msgs[2].Citations)
	}
	if title := repo.convs[turn.ConversationID()].Title; title != "Any novels about a desert planet…" {
		t.Fatalf("title %q", title)
	}

	// Second turn: history replayed, turn number increments, title kept.
	m.steps = []step{{tokens: []string{"Sure."}}}
	turn2, err := svc.Prepare(context.Background(), user, ChatRequest{ConversationID: turn.ConversationID().String(), Message: "Thanks", Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	sink2 := &recSink{}
	turn2.Run(context.Background(), sink2)
	if got := sequence(sink2.names()); got != "meta,token,citations,done" {
		t.Fatalf("turn 2 order %s", got)
	}
	if md := sink2.events[0].Data.(MetaData); md.Turn != 2 || md.Locale != LocaleEN {
		t.Fatalf("turn 2 meta %+v", md)
	}
	in := m.inputs[len(m.inputs)-1]
	if len(in) != 4 || in[1].Content != "Any novels about a desert planet with spice trade?" || in[2].Role != schema.Assistant {
		t.Fatalf("history window %d", len(in))
	}
}

func TestTurnLocaleZH(t *testing.T) {
	m := &scriptedModel{steps: []step{{tokens: []string{"沙丘"}}}}
	svc := newTestService(t, m, &fakeCatalog{}, newMemRepo(), fakeEmb{err: model.ErrEmbeddingUnavail})
	turn, err := svc.Prepare(context.Background(), uuid.New(), ChatRequest{Message: "有关于沙漠星球的小说吗", Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recSink{}
	turn.Run(context.Background(), sink)
	if md := sink.events[0].Data.(MetaData); md.Locale != LocaleZH {
		t.Fatalf("meta %+v", md)
	}
	if !strings.Contains(m.inputs[0][0].Content, "Simplified Chinese (zh-CN)") {
		t.Fatal("zh directive missing")
	}
	if got := sequence(sink.names()); got != "meta,token,citations,done" {
		t.Fatalf("order %s (RAG outage must not fail the turn)", got)
	}
}

func TestTurnUpstreamErrorMidStream(t *testing.T) {
	repo := newMemRepo()
	m := &scriptedModel{steps: []step{{tokens: []string{"Partial"}, err: model.New(model.CodeUpstreamError, "The AI provider failed mid-stream.")}}}
	svc := newTestService(t, m, &fakeCatalog{}, repo, fakeEmb{})
	turn, err := svc.Prepare(context.Background(), uuid.New(), ChatRequest{Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recSink{}
	turn.Run(context.Background(), sink)
	names := sink.names()
	if names[0] != EventMeta || names[len(names)-1] != EventError {
		t.Fatalf("order %v", names)
	}
	for _, n := range names {
		if n == EventDone || n == EventCitations {
			t.Fatalf("no events after error: %v", names)
		}
	}
	if e := sink.events[len(names)-1].Data.(ErrorData); e.Code != "UPSTREAM_ERROR" {
		t.Fatalf("error %+v", e)
	}
	if msgs := repo.msgs[turn.ConversationID()]; len(msgs) != 1 {
		t.Fatalf("only the user message persists: %d", len(msgs))
	}
}

func TestToolIterationCap(t *testing.T) {
	m := &scriptedModel{
		steps:    []step{toolStep("call_x", ToolCheckAvailability, `{"book_id":"`+duneID.String()+`"}`)},
		repeat:   true,
		toolless: step{tokens: []string{"Dune has 2 of 3 copies available."}},
	}
	svc := newTestService(t, m, &fakeCatalog{}, newMemRepo(), fakeEmb{})
	turn, err := svc.Prepare(context.Background(), uuid.New(), ChatRequest{Message: "Is Dune in?"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recSink{}
	turn.Run(context.Background(), sink)
	calls := 0
	for _, n := range sink.names() {
		if n == EventToolCall {
			calls++
		}
	}
	if calls != MaxToolIterations || sink.names()[len(sink.names())-1] != EventDone {
		t.Fatalf("calls=%d events=%v", calls, sink.names())
	}
	if last := m.choices[len(m.choices)-1]; last != schema.ToolChoiceForbidden {
		t.Fatalf("final call must forbid tools, got %q", last)
	}
	cits := sink.events[len(sink.events)-2].Data.(CitationsData).Citations
	if len(cits) != 1 || cits[0].Title != "Dune" || cits[0].CoverURL == nil || *cits[0].CoverURL != duneCover {
		t.Fatalf("tool-hit citation %+v", cits)
	}
}

func TestToolTimeoutBecomesObservation(t *testing.T) {
	old := toolTimeout
	toolTimeout = 30 * time.Millisecond
	defer func() { toolTimeout = old }()
	m := &scriptedModel{steps: []step{
		toolStep("call_1", ToolSearchCatalog, `{"query":"dune","mode":"keyword"}`),
		{tokens: []string{"The catalog is slow right now."}},
	}}
	svc := newTestService(t, m, &fakeCatalog{block: true}, newMemRepo(), fakeEmb{})
	turn, _ := svc.Prepare(context.Background(), uuid.New(), ChatRequest{Message: "dune?"})
	sink := &recSink{}
	turn.Run(context.Background(), sink)
	if got := sequence(sink.names()); got != "meta,tool_call,tool_result,token,citations,done" {
		t.Fatalf("order %s", got)
	}
	if s := sink.events[2].Data.(ToolResultData).Summary; s != "error: tool timed out" {
		t.Fatalf("summary %q", s)
	}
	last := m.inputs[1]
	if obs := last[len(last)-1]; obs.Role != schema.Tool || !strings.Contains(obs.Content, "timed out") {
		t.Fatalf("observation %+v", obs)
	}
}

func TestPrepareGuardrails(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	m := &scriptedModel{}
	svc := newTestService(t, m, &fakeCatalog{}, repo, fakeEmb{})
	owner, other := uuid.New(), uuid.New()
	conv, _ := repo.CreateConversation(ctx, uuid.New(), owner, model.DefaultConversationTitle)

	code := func(err error) string {
		de, ok := model.AsError(err)
		if !ok {
			return "?"
		}
		return string(de.Code)
	}
	if _, err := svc.Prepare(ctx, owner, ChatRequest{Message: "   "}); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("blank: %v", err)
	}
	if _, err := svc.Prepare(ctx, owner, ChatRequest{Message: strings.Repeat("a", MaxMessageRunes+1)}); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("long: %v", err)
	}
	if _, err := svc.Prepare(ctx, owner, ChatRequest{ConversationID: "nope", Message: "hi"}); code(err) != "NOT_FOUND" {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := svc.Prepare(ctx, other, ChatRequest{ConversationID: conv.ID.String(), Message: "hi"}); code(err) != "FORBIDDEN" {
		t.Fatalf("foreign: %v", err)
	}
	for i := 0; i < MessageCap-1; i++ {
		role := model.MsgRoleUser
		if i%2 == 1 {
			role = model.MsgRoleAssistant
		}
		_ = repo.AppendMessages(ctx, conv.ID, []model.ChatMessage{{ID: uuid.New(), Role: role, Content: strp("x")}})
	}
	_, err := svc.Prepare(ctx, owner, ChatRequest{ConversationID: conv.ID.String(), Message: "one more"})
	if code(err) != "RATE_LIMITED" || !strings.Contains(err.Error(), "start a new conversation") {
		t.Fatalf("cap: %v", err)
	}

	disabled := New(repo, fakeEmb{}, nil, Config{Enabled: true}, nil)
	_, err = disabled.Prepare(ctx, owner, ChatRequest{Message: "hi"})
	var de *model.Error
	if !errors.As(err, &de) || de.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("disabled: %v", err)
	}
}

func TestConversationCRUD(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := New(repo, fakeEmb{}, nil, Config{}, nil)
	u := uuid.New()
	c, err := svc.Create(ctx, u, nil)
	if err != nil || c.Title != model.DefaultConversationTitle {
		t.Fatalf("%+v %v", c, err)
	}
	named, _ := svc.Create(ctx, u, strp("  Babylonian math "))
	if named.Title != "Babylonian math" {
		t.Fatalf("title %q", named.Title)
	}
	p, err := svc.List(ctx, u, 0, 0)
	if err != nil || p.Meta.Total != 2 || p.Meta.PerPage != 20 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := svc.List(ctx, u, 1, 99); err == nil {
		t.Fatal("per_page cap")
	}
	if _, err := svc.Get(ctx, uuid.New(), c.ID.String()); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("foreign get %v", err)
	}
	if err := svc.Delete(ctx, u, c.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, u, c.ID.String()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted %v", err)
	}
}

func TestMarkerFilter(t *testing.T) {
	f := newMarkerFilter()
	var out strings.Builder
	for _, d := range []string{"See [", "doc#", "12] and [doc#3]", " [x] [doc#a] [doc#3]", " end ["} {
		out.WriteString(f.Push(d))
	}
	out.WriteString(f.Flush())
	if out.String() != "See  and  [x] [doc#a]  end [" || f.Text() != out.String() {
		t.Fatalf("%q", out.String())
	}
	if c := f.Cited(); len(c) != 2 || c[0] != 12 || c[1] != 3 {
		t.Fatalf("cited %v", c)
	}
}

func TestTitleAndWindow(t *testing.T) {
	cases := map[string]string{
		"Where can I find books on Babylonian mathematics?": "Where can I find books on…",
		"Dune please": "Dune please",
		"有关于沙漠星球的小说吗有关于沙漠星球的小说吗有关于": "有关于沙漠星球的小说吗有关于沙漠星球的小…",
		"   ": model.DefaultConversationTitle,
	}
	for in, want := range cases {
		if got := Title(in); got != want {
			t.Errorf("Title(%q) = %q, want %q", in, got, want)
		}
	}
	if EstimateTokens("abcdefgh") != 2 || EstimateTokens("沙丘") != 2 {
		t.Fatal("estimate")
	}
	hist := []HistoryMessage{{Content: strings.Repeat("a", 4000)}, {Content: strings.Repeat("b", 400)}, {Content: "c"}}
	kept := TrimHistory("sys", hist, "q", 200)
	if len(kept) != 2 || kept[0].Content[0] != 'b' {
		t.Fatalf("kept %d", len(kept))
	}
	if len(TrimHistory(strings.Repeat("s", 40000), hist, "q", 200)) != 0 {
		t.Fatal("oversized system keeps no history")
	}
}

func TestToolSchemasMatchDocs(t *testing.T) {
	re := regexp.MustCompile(`\s+`)
	for name := range toolSchemas {
		info, err := toolInfo(name)
		if err != nil {
			t.Fatal(err)
		}
		js, err := info.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(js)
		if !strings.Contains(re.ReplaceAllString(string(b), ""), `"required":[`) {
			t.Fatalf("%s schema %s", name, b)
		}
	}
}
