package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
)

var webm = []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81, 0x01}

// fakeWhisper is a scripted Transcriber recording what it received.
type fakeWhisper struct {
	mu     sync.Mutex
	out    ai.Transcript
	err    error
	block  bool
	got    ai.TranscribeInput
	called int
}

func (f *fakeWhisper) Transcribe(ctx context.Context, in ai.TranscribeInput) (ai.Transcript, error) {
	f.mu.Lock()
	f.got, f.called = in, f.called+1
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return ai.Transcript{}, ctx.Err()
	}
	return f.out, f.err
}

func transcribeApp(t *testing.T, stt ai.Transcriber, timeout time.Duration) (http.Handler, *http.Cookie) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Transcribe: ai.NewTranscribeService(stt, timeout, logging.Discard()),
		Limiter:    middleware.NewRateLimiter(rdb, logging.Discard()),
	})
	tok, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	return r, &http.Cookie{Name: middleware.AccessCookie, Value: tok}
}

func multipartBody(t *testing.T, audio []byte, locale string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if audio != nil {
		fw, _ := mw.CreateFormFile("audio", "clip.webm")
		_, _ = fw.Write(audio)
	}
	if locale != "" {
		_ = mw.WriteField("locale", locale)
	}
	_ = mw.Close()
	return &b, mw.FormDataContentType()
}

func postTranscribe(r http.Handler, c *http.Cookie, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/chat/transcribe", body)
	req.Header.Set("Content-Type", ct)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func envelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return m
}

func TestTranscribeHappyPath(t *testing.T) {
	stt := &fakeWhisper{out: ai.Transcript{Text: "where can I find books on Babylonian mathematics", Language: "en", Duration: 3 * time.Second}}
	r, cookie := transcribeApp(t, stt, 0)
	body, ct := multipartBody(t, webm, "zh-CN")
	rec := postTranscribe(r, cookie, body, ct)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := envelope(t, rec)
	if len(got) != 2 || got["text"] != "where can I find books on Babylonian mathematics" || got["language"] != "en" {
		t.Fatalf("body %v", got)
	}
	if !bytes.Equal(stt.got.Audio, webm) || stt.got.Locale != "zh-CN" || stt.got.Filename != "clip.webm" {
		t.Fatalf("adapter input %+v", stt.got)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("expected dedicated 10/min bucket, headers %v", rec.Header())
	}
}

func TestTranscribeSilentClipIs200Empty(t *testing.T) {
	r, cookie := transcribeApp(t, &fakeWhisper{out: ai.Transcript{Text: "", Language: "en"}}, 0)
	body, ct := multipartBody(t, webm, "")
	rec := postTranscribe(r, cookie, body, ct)
	got := envelope(t, rec)
	if rec.Code != 200 || got["text"] != "" || got["language"] != "en" {
		t.Fatalf("%d %v", rec.Code, got)
	}
}

func TestTranscribeRequiresAuth(t *testing.T) {
	stt := &fakeWhisper{}
	r, _ := transcribeApp(t, stt, 0)
	body, ct := multipartBody(t, webm, "")
	rec := postTranscribe(r, nil, body, ct)
	if rec.Code != 401 || envelope(t, rec)["error"] != "UNAUTHENTICATED" || stt.called != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestTranscribeValidation(t *testing.T) {
	stt := &fakeWhisper{out: ai.Transcript{Text: "x", Language: "en"}}
	r, cookie := transcribeApp(t, stt, 0)
	noAudio, noAudioCT := multipartBody(t, nil, "en")
	empty, emptyCT := multipartBody(t, []byte{}, "")
	text, textCT := multipartBody(t, []byte("hello, this is not audio"), "")
	huge, hugeCT := multipartBody(t, append(append([]byte{}, webm...), make([]byte, ai.MaxAudioBytes)...), "")
	cases := map[string]struct {
		body *bytes.Buffer
		ct   string
	}{
		"not multipart": {bytes.NewBufferString(`{"audio":"x"}`), "application/json"},
		"missing audio": {noAudio, noAudioCT},
		"empty audio":   {empty, emptyCT},
		"undecodable":   {text, textCT},
		"over size cap": {huge, hugeCT},
	}
	for name, tc := range cases {
		rec := postTranscribe(r, cookie, tc.body, tc.ct)
		if rec.Code != 400 || envelope(t, rec)["error"] != "VALIDATION_FAILED" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if stt.called != 0 {
		t.Fatalf("adapter reached %d times", stt.called)
	}
}

func TestTranscribeUpstreamErrors(t *testing.T) {
	cases := map[string]struct {
		stt    *fakeWhisper
		status int
		code   string
	}{
		"timeout":     {&fakeWhisper{block: true}, 503, "TRANSCRIPTION_UNAVAILABLE"},
		"down":        {&fakeWhisper{err: model.Wrap(model.CodeTranscriptionUnavailable, "down", context.Canceled)}, 503, "TRANSCRIPTION_UNAVAILABLE"},
		"undecodable": {&fakeWhisper{err: model.New(model.CodeValidationFailed, "The audio could not be decoded.")}, 400, "VALIDATION_FAILED"},
		"too long":    {&fakeWhisper{out: ai.Transcript{Text: "x", Duration: 2 * time.Minute}}, 400, "VALIDATION_FAILED"},
	}
	for name, tc := range cases {
		r, cookie := transcribeApp(t, tc.stt, 20*time.Millisecond)
		body, ct := multipartBody(t, webm, "en")
		rec := postTranscribe(r, cookie, body, ct)
		if rec.Code != tc.status || envelope(t, rec)["error"] != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestTranscribeRateBucketIsDedicated(t *testing.T) {
	stt := &fakeWhisper{out: ai.Transcript{Text: "x", Language: "en"}}
	r, cookie := transcribeApp(t, stt, 0)
	for i := 1; i <= 10; i++ {
		body, ct := multipartBody(t, webm, "")
		if rec := postTranscribe(r, cookie, body, ct); rec.Code != 200 {
			t.Fatalf("call %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	body, ct := multipartBody(t, webm, "")
	rec := postTranscribe(r, cookie, body, ct)
	if rec.Code != 429 || envelope(t, rec)["error"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("11th: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if stt.called != 10 {
		t.Fatalf("adapter called %d times; rate check must precede it", stt.called)
	}
	if strings.Contains(rec.Header().Get("X-RateLimit-Limit"), "120") {
		t.Fatal("general bucket must not govern transcribe")
	}
}

// gatedWhisper blocks each call until release is closed (signalling entered first).
type gatedWhisper struct {
	entered chan struct{}
	release chan struct{}
}

func (g *gatedWhisper) Transcribe(ctx context.Context, _ ai.TranscribeInput) (ai.Transcript, error) {
	g.entered <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ai.Transcript{}, ctx.Err()
	}
	return ai.Transcript{Text: "where can I find", Language: "en"}, nil
}

func partialApp(t *testing.T, final, fast ai.Transcriber) (http.Handler, *http.Cookie) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm,
		Transcribe: ai.NewTranscribeService(final, 0, logging.Discard()).WithPartial(fast, 0),
		Limiter:    middleware.NewRateLimiter(rdb, logging.Discard()),
	})
	tok, _, _ := jm.Issue(jwt.Claims{Subject: uuid.NewString(), Role: "user"})
	return r, &http.Cookie{Name: middleware.AccessCookie, Value: tok}
}

func postPartial(r http.Handler, c *http.Cookie, query string, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/chat/transcribe"+query, body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTranscribePartialUsesFastModelAndOwnBucket(t *testing.T) {
	final := &fakeWhisper{out: ai.Transcript{Text: "where can I find books on Babylonian mathematics", Language: "en"}}
	fast := &fakeWhisper{out: ai.Transcript{Text: "where can I find", Language: "en"}}
	r, cookie := partialApp(t, final, fast)

	body, ct := multipartBody(t, webm, "en")
	rec := postPartial(r, cookie, "?partial=true", body, ct)
	if rec.Code != 200 || envelope(t, rec)["text"] != "where can I find" {
		t.Fatalf("partial: %d %s", rec.Code, rec.Body)
	}
	if !fast.got.Partial || fast.got.Locale != "en" || final.called != 0 {
		t.Fatalf("routing: fast=%+v final.called=%d", fast.got, final.called)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "60" {
		t.Fatalf("expected 60/min partial bucket, headers %v", rec.Header())
	}
	// Burn the rest of the partial bucket: 60 allowed, the 61st is 429.
	for i := 2; i <= 60; i++ {
		body, ct := multipartBody(t, webm, "")
		if rec := postPartial(r, cookie, "?partial=1", body, ct); rec.Code != 200 {
			t.Fatalf("partial %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	body, ct = multipartBody(t, webm, "")
	rec = postPartial(r, cookie, "?partial=true", body, ct)
	if rec.Code != 429 || envelope(t, rec)["error"] != "RATE_LIMITED" {
		t.Fatalf("61st partial: %d %s", rec.Code, rec.Body)
	}
	// Partials never drain the 10/min final bucket.
	body, ct = multipartBody(t, webm, "")
	rec = postPartial(r, cookie, "?partial=false", body, ct)
	if rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "10" || rec.Header().Get("X-RateLimit-Remaining") != "9" {
		t.Fatalf("final after partials: %d %v", rec.Code, rec.Header())
	}
	if final.called != 1 || final.got.Partial {
		t.Fatalf("final routing: %d %+v", final.called, final.got)
	}
}

func TestTranscribePartialOneInFlightPerUser(t *testing.T) {
	gate := &gatedWhisper{entered: make(chan struct{}, 1), release: make(chan struct{})}
	final := &fakeWhisper{out: ai.Transcript{Text: "final", Language: "en"}}
	r, cookie := partialApp(t, final, gate)

	done := make(chan *httptest.ResponseRecorder)
	go func() {
		body, ct := multipartBody(t, webm, "")
		done <- postPartial(r, cookie, "?partial=true", body, ct)
	}()
	<-gate.entered

	body, ct := multipartBody(t, webm, "")
	rec := postPartial(r, cookie, "?partial=true", body, ct)
	if rec.Code != 429 || envelope(t, rec)["error"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrent partial: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	// The stop-time final pass is never blocked by an in-flight partial.
	body, ct = multipartBody(t, webm, "")
	if rec := postPartial(r, cookie, "", body, ct); rec.Code != 200 || envelope(t, rec)["text"] != "final" {
		t.Fatalf("final during partial: %d %s", rec.Code, rec.Body)
	}
	close(gate.release)
	if rec := <-done; rec.Code != 200 {
		t.Fatalf("holder: %d %s", rec.Code, rec.Body)
	}
	// Released: the next partial goes through.
	gate.release = make(chan struct{})
	close(gate.release)
	body, ct = multipartBody(t, webm, "")
	if rec := postPartial(r, cookie, "?partial=true", body, ct); rec.Code != 200 {
		t.Fatalf("after release: %d %s", rec.Code, rec.Body)
	}
}

func TestTranscribePartialValidation(t *testing.T) {
	fast := &fakeWhisper{out: ai.Transcript{Text: "x", Language: "en"}}
	r, cookie := partialApp(t, &fakeWhisper{}, fast)
	body, ct := multipartBody(t, webm, "")
	if rec := postPartial(r, cookie, "?partial=maybe", body, ct); rec.Code != 400 || envelope(t, rec)["error"] != "VALIDATION_FAILED" {
		t.Fatalf("bad flag: %d %s", rec.Code, rec.Body)
	}
	huge, hugeCT := multipartBody(t, append(append([]byte{}, webm...), make([]byte, ai.MaxAudioBytes)...), "")
	if rec := postPartial(r, cookie, "?partial=true", huge, hugeCT); rec.Code != 400 || envelope(t, rec)["error"] != "VALIDATION_FAILED" {
		t.Fatalf("partial size cap: %d %s", rec.Code, rec.Body)
	}
	if fast.called != 0 {
		t.Fatalf("adapter reached %d times", fast.called)
	}
}
