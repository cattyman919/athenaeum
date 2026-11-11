package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

func TestOpenRouterAdapterStream(t *testing.T) {
	var got oaRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			": OPENROUTER PROCESSING",
			`{"choices":[{"delta":{"content":"Checking"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"search_catalog","arguments":"{\"query\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"dune\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":50,"completion_tokens":7,"total_tokens":57}}`,
			"[DONE]",
		} {
			if line[0] == ':' {
				_, _ = fmt.Fprintf(w, "%s\n\n", line)
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
		}
	}))
	defer srv.Close()

	base := NewOpenRouter(OpenRouterConfig{BaseURL: srv.URL + "/api/v1/", APIKey: "sk-test", Model: "anthropic/claude-3.5-sonnet"})
	info, _ := toolInfo(ToolSearchCatalog)
	bound, err := base.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bound.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("dune?"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "c0", Function: schema.FunctionCall{Name: "search_catalog", Arguments: "{}"}}}},
		{Role: schema.Tool, ToolCallID: "c0", Content: `{"books":[]}`},
	}, einomodel.WithToolChoice(schema.ToolChoiceForbidden))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-test" || got.Model != "anthropic/claude-3.5-sonnet" || !got.Stream ||
		got.Temperature != DefaultTemperature || got.TopP != DefaultTopP || got.MaxTokens != DefaultMaxTokens {
		t.Fatalf("request %+v auth=%q", got, auth)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != ToolSearchCatalog || got.ToolChoice != "none" {
		t.Fatalf("tools %+v choice=%q", got.Tools, got.ToolChoice)
	}
	if len(got.Messages) != 4 || got.Messages[2].ToolCalls[0].ID != "c0" || got.Messages[3].ToolCallID != "c0" {
		t.Fatalf("messages %+v", got.Messages)
	}
	if msg.Content != "Checking" || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_9" ||
		msg.ToolCalls[0].Function.Arguments != `{"query":"dune"}` {
		t.Fatalf("message %+v", msg)
	}
	if msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil || msg.ResponseMeta.Usage.PromptTokens != 50 {
		t.Fatalf("usage %+v", msg.ResponseMeta)
	}
	// Unbound copy stays tool-free (immutability).
	if len(base.tools) != 0 {
		t.Fatal("WithTools mutated the base model")
	}
}

// Reasoning is requested with headroom on max_tokens, and streamed `reasoning` deltas
// become ReasoningContent; effort "none" sends no reasoning request.
func TestOpenRouterAdapterReasoning(t *testing.T) {
	var got oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = oaRequest{}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
			`{"choices":[{"delta":{"reasoning":"**Planning**\n\nSearch first.\n\n","reasoning_details":[{"type":"reasoning.text"}]}}]}`,
			`{"choices":[{"delta":{"reasoning":"**Answering**\n\nCite the shelf.\n\n"}}]}`,
			`{"choices":[{"delta":{"content":"Dune is on S-2."},"finish_reason":"stop"}]}`,
			"[DONE]",
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
		}
	}))
	defer srv.Close()

	m := NewOpenRouter(OpenRouterConfig{BaseURL: srv.URL, Model: "google/gemini-3.8-flash", ReasoningEffort: "medium"})
	msg, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("dune?")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reasoning == nil || got.Reasoning.Effort != "medium" || got.MaxTokens != DefaultMaxTokens+ReasoningHeadroom {
		t.Fatalf("reasoning request %+v max_tokens=%d", got.Reasoning, got.MaxTokens)
	}
	if msg.ReasoningContent != "**Planning**\n\nSearch first.\n\n**Answering**\n\nCite the shelf.\n\n" ||
		msg.Content != "Dune is on S-2." {
		t.Fatalf("message reasoning=%q content=%q", msg.ReasoningContent, msg.Content)
	}

	off := NewOpenRouter(OpenRouterConfig{BaseURL: srv.URL, Model: "google/gemini-3.8-flash", ReasoningEffort: ReasoningOff})
	if _, err := off.Generate(context.Background(), []*schema.Message{schema.UserMessage("dune?")}); err != nil {
		t.Fatal(err)
	}
	if got.Reasoning != nil || got.MaxTokens != DefaultMaxTokens {
		t.Fatalf("effort none must not request reasoning: %+v max_tokens=%d", got.Reasoning, got.MaxTokens)
	}
}

func TestOpenRouterAdapterErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("x") == "" && r.Header.Get("Authorization") == "Bearer bad" {
			http.Error(w, `{"error":{"message":"invalid key"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"overloaded\"}}\n\n")
	}))
	defer srv.Close()

	_, err := NewOpenRouter(OpenRouterConfig{BaseURL: srv.URL, APIKey: "bad"}).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if !errors.Is(err, model.ErrUpstream) {
		t.Fatalf("status error: %v", err)
	}
	_, err = NewOpenRouter(OpenRouterConfig{BaseURL: srv.URL, APIKey: "ok"}).Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if !errors.Is(err, model.ErrUpstream) {
		t.Fatalf("mid-stream error: %v", err)
	}
	_, err = NewOpenRouter(OpenRouterConfig{BaseURL: "http://127.0.0.1:1", APIKey: "ok"}).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if !errors.Is(err, model.ErrUpstream) {
		t.Fatalf("unreachable: %v", err)
	}
}
