package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// This file is the anti-corruption layer between the agent and the chat vendor
// (docs/06 §5): an OpenAI-compatible streaming client (OpenRouter, or the
// api/cmd/e2e-stubs fake) exposed as an Eino ToolCallingChatModel. Swapping vendors
// touches only this file. The speech-to-text adapter (WhisperAdapter, docs/15 §5)
// lives at the bottom under the same rule.

// Generation defaults (docs/06 §3, §8).
const (
	DefaultTemperature = 0.3
	DefaultTopP        = 0.9
	DefaultMaxTokens   = 1024
	connectTimeout     = 5 * time.Second
	streamTimeout      = 120 * time.Second
)

// OpenRouterConfig configures the ACL chat model.
type OpenRouterConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32
	TopP        float32
	MaxTokens   int
	// HTTPClient overrides the default client (5 s connect / 120 s stream, docs/01 §5).
	HTTPClient *http.Client
}

// ACLChatModel adapts an OpenAI-compatible /chat/completions endpoint to
// einomodel.ToolCallingChatModel. It is immutable; WithTools returns a copy.
type ACLChatModel struct {
	cfg   OpenRouterConfig
	http  *http.Client
	tools []*schema.ToolInfo
}

var _ einomodel.ToolCallingChatModel = (*ACLChatModel)(nil)

// NewOpenRouter builds the adapter, filling generation defaults.
func NewOpenRouter(cfg OpenRouterConfig) *ACLChatModel {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Temperature == 0 {
		cfg.Temperature = DefaultTemperature
	}
	if cfg.TopP == 0 {
		cfg.TopP = DefaultTopP
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Timeout: streamTimeout,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: connectTimeout}).DialContext,
				TLSHandshakeTimeout: connectTimeout,
			},
		}
	}
	return &ACLChatModel{cfg: cfg, http: hc}
}

// WithTools returns a copy bound to the given tools.
func (m *ACLChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	c := *m
	c.tools = tools
	return &c, nil
}

// Generate returns the whole reply (built from the stream).
func (m *ACLChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	sr, err := m.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	defer sr.Close()
	var chunks []*schema.Message
	for {
		c, err := sr.Recv()
		if err == io.EOF { //nolint:errorlint // io.EOF is returned unwrapped by StreamReader
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	if len(chunks) == 0 {
		return &schema.Message{Role: schema.Assistant}, nil
	}
	return schema.ConcatMessages(chunks)
}

// --- wire types (OpenAI chat completions) ---

type oaFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type oaToolCall struct {
	Index    *int       `json:"index,omitempty"`
	ID       string     `json:"id,omitempty"`
	Type     string     `json:"type,omitempty"`
	Function oaFunction `json:"function"`
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaRequest struct {
	Model         string         `json:"model"`
	Messages      []oaMessage    `json:"messages"`
	Tools         []oaTool       `json:"tools,omitempty"`
	ToolChoice    string         `json:"tool_choice,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions map[string]any `json:"stream_options,omitempty"`
	Temperature   float32        `json:"temperature"`
	TopP          float32        `json:"top_p"`
	MaxTokens     int            `json:"max_tokens"`
}

type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content   string       `json:"content"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func upstream(msg string, cause error) error {
	return model.Wrap(model.CodeUpstreamError, msg, cause)
}

func (m *ACLChatModel) buildRequest(input []*schema.Message, opts ...einomodel.Option) (*oaRequest, error) {
	o := einomodel.GetCommonOptions(&einomodel.Options{
		Temperature: &m.cfg.Temperature, TopP: &m.cfg.TopP, MaxTokens: &m.cfg.MaxTokens,
		Model: &m.cfg.Model, Tools: m.tools,
	}, opts...)
	req := &oaRequest{
		Model: *o.Model, Stream: true, StreamOptions: map[string]any{"include_usage": true},
		Temperature: *o.Temperature, TopP: *o.TopP, MaxTokens: *o.MaxTokens,
	}
	for _, msg := range input {
		om := oaMessage{Role: string(msg.Role), Content: msg.Content, ToolCallID: msg.ToolCallID}
		for _, tc := range msg.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, oaToolCall{ID: tc.ID, Type: "function",
				Function: oaFunction{Name: tc.Function.Name, Arguments: tc.Function.Arguments}})
		}
		req.Messages = append(req.Messages, om)
	}
	for _, t := range o.Tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description = t.Name, t.Desc
		ot.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		if t.ParamsOneOf != nil {
			js, err := t.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("tool %s schema: %w", t.Name, err)
			}
			if raw, err := json.Marshal(js); err == nil {
				ot.Function.Parameters = raw
			}
		}
		req.Tools = append(req.Tools, ot)
	}
	if len(req.Tools) > 0 && o.ToolChoice != nil {
		switch *o.ToolChoice {
		case schema.ToolChoiceForbidden:
			req.ToolChoice = "none"
		case schema.ToolChoiceForced:
			req.ToolChoice = "required"
		default:
			req.ToolChoice = "auto"
		}
	}
	return req, nil
}

// Stream sends a streaming chat completion and converts SSE chunks into Eino messages.
// Upstream failures surface as UPSTREAM_ERROR domain errors.
func (m *ACLChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	body, err := m.buildRequest(input, opts...)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if m.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	}
	req.Header.Set("X-Title", "Athenaeum")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, upstream("The AI provider is unreachable.", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		return nil, upstream("The AI provider returned an error.",
			fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b))))
	}
	sr, sw := schema.Pipe[*schema.Message](16)
	go func() {
		defer sw.Close()
		defer func() { _ = resp.Body.Close() }()
		readSSE(resp.Body, sw)
	}()
	return sr, nil
}

func readSSE(r io.Reader, sw *schema.StreamWriter[*schema.Message]) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return
		}
		var ch oaChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			sw.Send(nil, upstream("Malformed AI provider stream.", err))
			return
		}
		if ch.Error != nil {
			sw.Send(nil, upstream("The AI provider failed mid-stream.", fmt.Errorf("%s", ch.Error.Message)))
			return
		}
		msg := &schema.Message{Role: schema.Assistant}
		if len(ch.Choices) > 0 {
			c := ch.Choices[0]
			msg.Content = c.Delta.Content
			for i, tc := range c.Delta.ToolCalls {
				idx := i
				if tc.Index != nil {
					idx = *tc.Index
				}
				typ := tc.Type
				if typ == "" && tc.ID != "" {
					typ = "function"
				}
				msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{Index: &idx, ID: tc.ID, Type: typ,
					Function: schema.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments}})
			}
			if c.FinishReason != nil {
				msg.ResponseMeta = &schema.ResponseMeta{FinishReason: *c.FinishReason}
			}
		}
		if ch.Usage != nil {
			if msg.ResponseMeta == nil {
				msg.ResponseMeta = &schema.ResponseMeta{}
			}
			msg.ResponseMeta.Usage = &schema.TokenUsage{PromptTokens: ch.Usage.PromptTokens,
				CompletionTokens: ch.Usage.CompletionTokens, TotalTokens: ch.Usage.TotalTokens}
		}
		if msg.Content == "" && len(msg.ToolCalls) == 0 && msg.ResponseMeta == nil {
			continue
		}
		if sw.Send(msg, nil) {
			return // reader closed
		}
	}
	if err := sc.Err(); err != nil {
		sw.Send(nil, upstream("The AI provider stream was interrupted.", err))
	}
}

// --- Speech-to-text ACL (docs/15 §5) ---
//
// WhisperAdapter speaks the onerahmet/openai-whisper-asr-webservice API (ASR_ENGINE=
// faster_whisper, pinned in docker-compose.yml): POST {base}/asr with a multipart
// `audio_file` part and query `task=transcribe&output=json&encode=true&vad_filter=true
// [&language=en|zh]`. The JSON body is {"text","language","segments":[{"start","end",…}]}
// (served as text/plain). api/cmd/e2e-stubs fakes the same contract. Swapping STT
// vendors touches only this section.

// WhisperConfig configures the STT adapter. The model itself (WHISPER_MODEL) is chosen
// by the whisper container (ASR_MODEL), not per request.
type WhisperConfig struct {
	BaseURL string
	// HTTPClient overrides the default client (5 s connect; the per-call deadline comes
	// from the caller's context, docs/15 §6).
	HTTPClient *http.Client
}

// WhisperAdapter implements Transcriber against the whisper ASR webservice.
type WhisperAdapter struct {
	base string
	http *http.Client
}

var _ Transcriber = (*WhisperAdapter)(nil)

// NewWhisper builds the STT adapter.
func NewWhisper(cfg WhisperConfig) *WhisperAdapter {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{Timeout: connectTimeout}).DialContext,
		}}
	}
	return &WhisperAdapter{base: strings.TrimRight(cfg.BaseURL, "/"), http: hc}
}

// WhisperLanguage maps a UI locale to the Whisper `language` hint (docs/15 §4);
// anything else → "" (auto-detect).
func WhisperLanguage(locale string) string {
	switch locale {
	case LocaleEN:
		return "en"
	case LocaleZH:
		return "zh"
	}
	return ""
}

type whisperResponse struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	Segments []struct {
		End float64 `json:"end"`
	} `json:"segments"`
}

func sttUnavailable(cause error) error {
	return model.Wrap(model.CodeTranscriptionUnavailable, "Voice transcription is temporarily unavailable.", cause)
}

// Transcribe uploads the clip and returns the transcript. Connection failures,
// timeouts and 5xx gateway statuses → TRANSCRIPTION_UNAVAILABLE; 4xx and 500 (the
// service raises 500 when ffmpeg cannot decode the clip) → VALIDATION_FAILED. Error
// causes never include audio bytes or transcript text.
func (w *WhisperAdapter) Transcribe(ctx context.Context, in TranscribeInput) (Transcript, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	name := in.Filename
	if name == "" {
		name = "audio.webm"
	}
	part, err := mw.CreateFormFile("audio_file", name)
	if err != nil {
		return Transcript{}, err
	}
	if _, err := part.Write(in.Audio); err != nil {
		return Transcript{}, err
	}
	if err := mw.Close(); err != nil {
		return Transcript{}, err
	}
	q := url.Values{"task": {"transcribe"}, "output": {"json"}, "encode": {"true"}, "vad_filter": {"true"}}
	if lang := WhisperLanguage(in.Locale); lang != "" {
		q.Set("language", lang)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/asr?"+q.Encode(), &body)
	if err != nil {
		return Transcript{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return Transcript{}, sttUnavailable(err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 400 && resp.StatusCode <= 500:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Transcript{}, model.Wrap(model.CodeValidationFailed, "The audio could not be decoded.",
			fmt.Errorf("whisper status %d", resp.StatusCode))
	default:
		return Transcript{}, sttUnavailable(fmt.Errorf("whisper status %d", resp.StatusCode))
	}
	var out whisperResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Transcript{}, sttUnavailable(fmt.Errorf("decode whisper response: %w", err))
	}
	var end float64
	for _, s := range out.Segments {
		end = max(end, s.End)
	}
	lang := out.Language
	if lang == "" {
		lang = WhisperLanguage(in.Locale)
	}
	return Transcript{
		Text:     strings.TrimSpace(out.Text),
		Language: lang,
		Duration: time.Duration(end * float64(time.Second)),
	}, nil
}
