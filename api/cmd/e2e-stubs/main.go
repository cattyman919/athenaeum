// Command e2e-stubs serves deterministic stand-ins for the external AI providers used
// by the Playwright suite (docs/09 §8.1): an OpenAI-compatible streaming
// /chat/completions (point OPENROUTER_BASE_URL at it) and a fixed-text Whisper /asr
// (point WHISPER_BASE_URL at it). No secrets, no network.
//
// Chat script (drives the real Eino agent + tools + SSE pipeline):
//  1. last message is the user's and tools are offered → one search_catalog tool call
//     with the user's text as a keyword query;
//  2. a tool result is present → a streamed answer naming the first book found, in
//     Simplified Chinese when the system prompt asks for zh-CN.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// StubTranscript is the fixed /asr transcript.
const StubTranscript = "where can I find books on Babylonian mathematics"

func main() {
	addr := flag.String("addr", envOr("E2E_STUBS_ADDR", ":18090"), "listen address")
	flag.Parse()
	srv := &http.Server{Addr: *addr, Handler: newMux(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("e2e-stubs listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /chat/completions", chatCompletions)
	mux.HandleFunc("POST /v1/chat/completions", chatCompletions)
	mux.HandleFunc("POST /asr", asr)
	mux.HandleFunc("GET /api/books", openLibraryBooks) // OPENLIBRARY_BASE_URL (openlibrary.go)
	return mux
}

// asr mirrors onerahmet/openai-whisper-asr-webservice v1.10.0 POST /asr with
// output=json (docs/15 §5): a multipart `audio_file` part is required (FastAPI
// answers 422 otherwise); the body is {"text","language","segments"} served as
// text/plain, with Whisper's leading space on the text.
func asr(w http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("audio_file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"type":"missing","loc":["body","audio_file"],"msg":"Field required"}]}`))
		return
	}
	_ = f.Close()
	if o := r.URL.Query().Get("output"); o != "" && o != "json" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintln(w, StubTranscript)
		return
	}
	lang := r.URL.Query().Get("language")
	if lang == "" {
		lang = "en" // "detected"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Asr-Engine", "faster_whisper")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"text":     " " + StubTranscript,
		"language": lang,
		"segments": []map[string]any{{"id": 1, "seek": 0, "start": 0.0, "end": 2.4, "text": " " + StubTranscript}},
	})
}

type message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

type request struct {
	Messages []message        `json:"messages"`
	Tools    []map[string]any `json:"tools"`
	Stream   bool             `json:"stream"`
}

func chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, _ := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	zh := strings.Contains(req.Messages[0].Content, "Simplified Chinese")
	last := req.Messages[len(req.Messages)-1]

	if last.Role == "user" && len(req.Tools) > 0 {
		args, _ := json.Marshal(map[string]any{"query": keywordQuery(last.Content), "mode": "keyword", "top_k": 3})
		send(delta(map[string]any{"role": "assistant", "content": ""}, nil))
		send(delta(map[string]any{"tool_calls": []map[string]any{{"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": "search_catalog", "arguments": ""}}}}, nil))
		send(delta(map[string]any{"tool_calls": []map[string]any{{"index": 0,
			"function": map[string]any{"arguments": string(args)}}}}, nil))
		send(delta(map[string]any{}, ptr("tool_calls")))
		send(usage(120, 18))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	title := firstBookTitle(req.Messages)
	var text string
	switch {
	case title == "" && zh:
		text = "抱歉，我在 Athenaeum 馆藏中没有找到匹配的图书。"
	case title == "":
		text = "I could not find a matching book in the Athenaeum catalog."
	case zh:
		text = "我在 Athenaeum 馆藏中找到了：" + title + "。"
	default:
		text = "From the Athenaeum catalog, I recommend " + title + "."
	}
	for _, part := range chunks(text) {
		send(delta(map[string]any{"content": part}, nil))
	}
	send(delta(map[string]any{}, ptr("stop")))
	send(usage(240, 32))
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// keywordQuery keeps the longest word (a robust single-term keyword query).
func keywordQuery(s string) string {
	best := ""
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '?' || r == ',' || r == '.' || r == '!'
	}) {
		if len([]rune(f)) > len([]rune(best)) {
			best = f
		}
	}
	if best == "" {
		return s
	}
	return best
}

func firstBookTitle(msgs []message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "tool" {
			continue
		}
		var out struct {
			Books []struct {
				Title string `json:"title"`
			} `json:"books"`
		}
		if json.Unmarshal([]byte(msgs[i].Content), &out) == nil && len(out.Books) > 0 {
			return out.Books[0].Title
		}
		return ""
	}
	return ""
}

func chunks(s string) []string {
	r := []rune(s)
	var out []string
	for len(r) > 0 {
		n := min(8, len(r))
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

func ptr(s string) *string { return &s }

func delta(d map[string]any, finish *string) map[string]any {
	return map[string]any{
		"id": "stub", "object": "chat.completion.chunk", "model": "stub/fake-chat",
		"choices": []map[string]any{{"index": 0, "delta": d, "finish_reason": finish}},
	}
}

func usage(in, out int) map[string]any {
	return map[string]any{
		"id": "stub", "object": "chat.completion.chunk", "choices": []any{},
		"usage": map[string]int{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out},
	}
}
