package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

type stubCatalog struct{ queries []string }

func (c *stubCatalog) Search(_ context.Context, req catalog.Request) (model.SearchPage, error) {
	c.queries = append(c.queries, req.Mode+":"+req.Query)
	return model.SearchPage{Items: []model.BookSummary{{ID: uuid.New(), Title: "The Exact Sciences in Antiquity"}}}, nil
}

func (c *stubCatalog) Detail(context.Context, string, uuid.UUID) (model.BookDetail, error) {
	return model.BookDetail{}, model.ErrNotFound
}

// The real ACL adapter + Eino agent against the stub: tool call, then a streamed answer.
func TestStubDrivesAgent(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()
	cat := &stubCatalog{}
	eng, err := ai.NewEinoEngine(context.Background(),
		ai.NewOpenRouter(ai.OpenRouterConfig{BaseURL: srv.URL, APIKey: "stub", Model: "stub/fake-chat"}), cat)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ locale, want string }{
		{ai.LocaleEN, "From the Athenaeum catalog, I recommend The Exact Sciences in Antiquity."},
		{ai.LocaleZH, "我在 Athenaeum 馆藏中找到了：The Exact Sciences in Antiquity。"},
	} {
		var events []string
		var text strings.Builder
		res, err := eng.Run(context.Background(), uuid.New(), ai.EngineInput{
			System: ai.SystemPrompt(tc.locale, ""), User: "Where can I find books on Babylonian mathematics?",
		}, func(e ai.Event) { events = append(events, e.Name) }, func(s string) { text.WriteString(s) })
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(events, ",") != "tool_call,tool_result" || text.String() != tc.want {
			t.Fatalf("%s: events=%v text=%q", tc.locale, events, text.String())
		}
		if res.TokensIn != 360 || res.TokensOut != 50 || len(res.Tools) != 1 {
			t.Fatalf("result %+v", res)
		}
	}
	if cat.queries[0] != "keyword:mathematics" {
		t.Fatalf("queries %v", cat.queries)
	}
}

func TestStubASR(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/asr?language=zh", "audio/webm", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["text"] != StubTranscript || out["language"] != "zh" {
		t.Fatalf("%v", out)
	}
}
