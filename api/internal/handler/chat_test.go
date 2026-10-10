package handler_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
)

type chatRepo struct {
	mu    sync.Mutex
	convs map[uuid.UUID]model.Conversation
	msgs  map[uuid.UUID][]model.ChatMessage
}

func newChatRepo() *chatRepo {
	return &chatRepo{convs: map[uuid.UUID]model.Conversation{}, msgs: map[uuid.UUID][]model.ChatMessage{}}
}

func (r *chatRepo) RetrieveChunks(context.Context, []float32, int, int, float64) ([]model.RAGChunk, error) {
	p, cover := "MAIN/3F/SF/A-1/S-2", "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg"
	return []model.RAGChunk{{BookID: uuid.MustParse("8c140000-0000-4000-8000-000000000001"), Title: "Dune",
		Content: "Arrakis.", Path: &p, Score: 0.86, CoverURL: &cover}}, nil
}

func (r *chatRepo) CreateConversation(_ context.Context, id, u uuid.UUID, title string) (model.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := model.Conversation{ID: id, UserID: u, Title: title, UpdatedAt: time.Now()}
	r.convs[id] = c
	return c, nil
}

func (r *chatRepo) Conversation(_ context.Context, id uuid.UUID) (model.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.convs[id]; ok {
		return c, nil
	}
	return model.Conversation{}, model.New(model.CodeNotFound, "Conversation not found.")
}

func (r *chatRepo) ListConversations(_ context.Context, u uuid.UUID, _, _ int) ([]model.Conversation, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.Conversation{}
	for _, c := range r.convs {
		if c.UserID == u {
			out = append(out, c)
		}
	}
	return out, len(out), nil
}

func (r *chatRepo) DeleteConversation(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.convs, id)
	return nil
}

func (r *chatRepo) SetTitle(_ context.Context, id uuid.UUID, t string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.convs[id]
	c.Title = t
	r.convs[id] = c
	return nil
}

func (r *chatRepo) Messages(_ context.Context, id uuid.UUID) ([]model.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.ChatMessage(nil), r.msgs[id]...), nil
}

func (r *chatRepo) AppendMessages(_ context.Context, id uuid.UUID, m []model.ChatMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs[id] = append(r.msgs[id], m...)
	return nil
}

type okEmb struct{}

func (okEmb) EmbedQuery(context.Context, string) ([]float32, error) { return []float32{1}, nil }

// fakeEngine is a scripted engine: tool events, a pause (heartbeat window), tokens.
type fakeEngine struct{ pause time.Duration }

func (e fakeEngine) Run(_ context.Context, _ uuid.UUID, in ai.EngineInput, emit func(ai.Event), onToken func(string)) (ai.EngineResult, error) {
	emit(ai.Event{Name: ai.EventToolCall, Data: ai.ToolCallData{ID: "call_1", Name: "search_catalog", Args: json.RawMessage(`{"query":"dune"}`)}})
	emit(ai.Event{Name: ai.EventToolResult, Data: ai.ToolResultData{ID: "call_1", Summary: "1 book found"}})
	time.Sleep(e.pause)
	answer := "Dune is on MAIN/3F/SF/A-1/S-2 [doc#1]."
	if strings.Contains(in.System, "Simplified Chinese") {
		answer = "《沙丘》在 MAIN/3F/SF/A-1/S-2 [doc#1]。"
	}
	ra := []rune(answer)
	for _, part := range []string{string(ra[:10]), string(ra[10:])} {
		onToken(part)
	}
	return ai.EngineResult{TokensIn: 842, TokensOut: 210}, nil
}

func chatApp(t *testing.T, enabled bool) (http.Handler, *http.Cookie, *chatRepo) {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	repo := newChatRepo()
	var eng ai.Engine
	if enabled {
		eng = fakeEngine{pause: 40 * time.Millisecond}
	}
	svc := ai.New(repo, okEmb{}, eng, ai.Config{Enabled: enabled, Model: "anthropic/claude-3.5-sonnet"}, logging.Discard())
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm, Chat: svc, ChatHeartbeat: 10 * time.Millisecond,
	})
	tok, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	return r, &http.Cookie{Name: middleware.AccessCookie, Value: tok}, repo
}

type sseEvent struct {
	name string
	data map[string]any
}

func parseSSE(t *testing.T, body string) ([]sseEvent, int) {
	t.Helper()
	var out []sseEvent
	pings := 0
	sc := bufio.NewScanner(strings.NewReader(body))
	var cur sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == ": ping":
			pings++
		case strings.HasPrefix(line, "event: "):
			cur = sseEvent{name: strings.TrimPrefix(line, "event: ")}
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("bad data line %q", line)
			}
		case line == "" && cur.name != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out, pings
}

func postChat(r http.Handler, c *http.Cookie, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestChatSSEWireContract(t *testing.T) {
	r, cookie, repo := chatApp(t, true)
	rec := postChat(r, cookie, `{"message":"Where is Dune?","locale":"zh-CN"}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	h := rec.Header()
	if h.Get("Content-Type") != "text/event-stream" || h.Get("Cache-Control") != "no-cache" || h.Get("Connection") != "keep-alive" {
		t.Fatalf("headers %v", h)
	}
	events, pings := parseSSE(t, rec.Body.String())
	var names []string
	for _, e := range events {
		names = append(names, e.name)
	}
	want := "meta,tool_call,tool_result,token,token,citations,done"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("order\n got %s\nwant %s", got, want)
	}
	if pings == 0 {
		t.Fatal("expected heartbeat comments")
	}
	meta := events[0].data
	if meta["locale"] != "zh-CN" || meta["turn"] != float64(1) || meta["model"] != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("meta %v", meta)
	}
	if _, err := uuid.Parse(meta["conversation_id"].(string)); err != nil {
		t.Fatalf("conversation_id %v", meta)
	}
	text := events[3].data["text"].(string) + events[4].data["text"].(string)
	if strings.Contains(text, "[doc#") {
		t.Fatalf("marker leaked: %q", text)
	}
	cits := events[5].data["citations"].([]any)
	c0 := cits[0].(map[string]any)
	if len(cits) != 1 || c0["title"] != "Dune" || c0["path"] != "MAIN/3F/SF/A-1/S-2" || c0["score"] != 0.86 ||
		c0["cover_url"] != "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg" {
		t.Fatalf("citations %v", cits)
	}
	done := events[6].data
	if done["tokens_in"] != float64(842) || done["tokens_out"] != float64(210) || done["finish_reason"] != "stop" {
		t.Fatalf("done %v", done)
	}

	// Conversation CRUD sees the implicit conversation with a derived title.
	cid := meta["conversation_id"].(string)
	g := get(r, "/api/chat/conversations/"+cid, cookie)
	var detail struct {
		Title    string           `json:"title"`
		Messages []map[string]any `json:"messages"`
	}
	_ = json.Unmarshal(g.Body.Bytes(), &detail)
	if g.Code != 200 || detail.Title != "Where is Dune?" || len(detail.Messages) != 2 {
		t.Fatalf("detail %d %s", g.Code, g.Body)
	}
	if detail.Messages[1]["id"] != done["message_id"] || detail.Messages[1]["role"] != "assistant" {
		t.Fatalf("assistant id mismatch %v", detail.Messages[1])
	}
	if _, ok := detail.Messages[0]["tool_meta"]; !ok {
		t.Fatal("tool_meta key must be present (null)")
	}
	// Assistant rows replay the persisted citation cards; other roles carry none.
	if _, ok := detail.Messages[0]["citations"]; ok {
		t.Fatalf("user row must not carry citations %v", detail.Messages[0])
	}
	hc, _ := detail.Messages[1]["citations"].([]any)
	if len(hc) != 1 || hc[0].(map[string]any)["title"] != "Dune" ||
		hc[0].(map[string]any)["cover_url"] != c0["cover_url"] {
		t.Fatalf("history citations %v", detail.Messages[1])
	}
	_ = repo
}

func TestChatPreStreamErrorsAreJSON(t *testing.T) {
	r, cookie, _ := chatApp(t, true)
	cases := map[string]struct {
		body   string
		status int
		code   string
	}{
		"blank":   {`{"message":" "}`, 400, "VALIDATION_FAILED"},
		"bad":     {`{`, 400, "VALIDATION_FAILED"},
		"unknown": {`{"message":"hi","conversation_id":"` + uuid.NewString() + `"}`, 404, "NOT_FOUND"},
	}
	for name, tc := range cases {
		rec := postChat(r, cookie, tc.body)
		var env map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if rec.Code != tc.status || env["error"] != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := postChat(r, nil, `{"message":"hi"}`); rec.Code != 401 {
		t.Fatalf("anon %d", rec.Code)
	}

	off, offCookie, _ := chatApp(t, false)
	rec := postChat(off, offCookie, `{"message":"hi"}`)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"UPSTREAM_ERROR"`) {
		t.Fatalf("unconfigured %d %s", rec.Code, rec.Body)
	}
}

func TestChatConversationEndpoints(t *testing.T) {
	r, cookie, _ := chatApp(t, true)
	req := httptest.NewRequest("POST", "/api/chat/conversations", strings.NewReader(`{"title":"Babylon"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var c map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if rec.Code != 201 || c["title"] != "Babylon" || c["updated_at"] == nil {
		t.Fatalf("create %d %s", rec.Code, rec.Body)
	}
	empty := httptest.NewRequest("POST", "/api/chat/conversations", nil)
	empty.AddCookie(cookie)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, empty)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), model.DefaultConversationTitle) {
		t.Fatalf("create empty %d %s", rec.Code, rec.Body)
	}
	l := get(r, "/api/chat/conversations", cookie)
	if l.Code != 200 || !strings.Contains(l.Body.String(), `"total":2`) {
		t.Fatalf("list %d %s", l.Code, l.Body)
	}
	del := httptest.NewRequest("DELETE", "/api/chat/conversations/"+c["id"].(string), nil)
	del.AddCookie(cookie)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, del)
	if rec.Code != 204 {
		t.Fatalf("delete %d", rec.Code)
	}
	if g := get(r, "/api/chat/conversations/"+c["id"].(string), cookie); g.Code != 404 {
		t.Fatalf("after delete %d", g.Code)
	}
}
