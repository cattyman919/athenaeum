package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
)

var webmMagic = []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81}

func TestWhisperLanguage(t *testing.T) {
	for in, want := range map[string]string{"en": "en", "zh-CN": "zh", "": "", "fr": "", "zh": ""} {
		if got := WhisperLanguage(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestWhisperAdapterRequestAndResponse(t *testing.T) {
	var gotQuery, gotFile, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/asr" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		f, h, err := r.FormFile("audio_file")
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		b, _ := io.ReadAll(f)
		gotFile, gotName = string(b), h.Filename
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, `{"language":"zh","segments":[{"id":1,"start":0.0,"end":3.5,"text":" 你好"},{"id":2,"start":3.5,"end":7.25,"text":"世界"}],"text":" 你好世界 "}`)
	}))
	defer srv.Close()

	w := NewWhisper(WhisperConfig{BaseURL: srv.URL + "/"})
	out, err := w.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic, Filename: "clip.webm", Locale: LocaleZH})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "你好世界" || out.Language != "zh" || out.Duration != 7250*time.Millisecond {
		t.Fatalf("%+v", out)
	}
	if gotFile != string(webmMagic) || gotName != "clip.webm" {
		t.Fatalf("upload %q %q", gotFile, gotName)
	}
	for _, p := range []string{"task=transcribe", "output=json", "encode=true", "vad_filter=true", "language=zh"} {
		if !strings.Contains(gotQuery, p) {
			t.Errorf("query %q missing %s", gotQuery, p)
		}
	}

	// No locale → no language hint (auto-detect).
	if _, err := w.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQuery, "language=") || gotName != "audio.webm" {
		t.Fatalf("query %q name %q", gotQuery, gotName)
	}
}

func TestWhisperAdapterErrorMapping(t *testing.T) {
	cases := map[int]model.Code{
		http.StatusUnprocessableEntity: model.CodeValidationFailed,
		http.StatusInternalServerError: model.CodeValidationFailed, // ffmpeg decode failure
		http.StatusBadGateway:          model.CodeTranscriptionUnavailable,
		http.StatusServiceUnavailable:  model.CodeTranscriptionUnavailable,
	}
	for status, code := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Internal Server Error", status)
		}))
		_, err := NewWhisper(WhisperConfig{BaseURL: srv.URL}).Transcribe(context.Background(), TranscribeInput{Audio: webmMagic})
		srv.Close()
		if de, ok := model.AsError(err); !ok || de.Code != code {
			t.Errorf("%d: %v", status, err)
		}
	}
	// Malformed body → unavailable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "nope") }))
	_, err := NewWhisper(WhisperConfig{BaseURL: srv.URL}).Transcribe(context.Background(), TranscribeInput{Audio: webmMagic})
	srv.Close()
	if !errors.Is(err, model.ErrTranscriptionUnav) {
		t.Errorf("malformed: %v", err)
	}
	// Service down → unavailable.
	_, err = NewWhisper(WhisperConfig{BaseURL: srv.URL}).Transcribe(context.Background(), TranscribeInput{Audio: webmMagic})
	if !errors.Is(err, model.ErrTranscriptionUnav) {
		t.Errorf("down: %v", err)
	}
}

type fakeSTT struct {
	out   Transcript
	err   error
	delay time.Duration
	calls int
}

func (f *fakeSTT) Transcribe(ctx context.Context, _ TranscribeInput) (Transcript, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return Transcript{}, ctx.Err()
		}
	}
	return f.out, f.err
}

func TestTranscribeServiceValidation(t *testing.T) {
	stt := &fakeSTT{out: Transcript{Text: "hi", Language: "en", Duration: time.Second}}
	s := NewTranscribeService(stt, 0, logging.Discard())
	for name, audio := range map[string][]byte{
		"empty":   nil,
		"toolong": make([]byte, MaxAudioBytes+1),
		"text":    []byte("definitely not audio"),
	} {
		if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: audio}); !errors.Is(err, model.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if stt.calls != 0 {
		t.Fatalf("adapter called %d times for invalid input", stt.calls)
	}
	out, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic})
	if err != nil || out.Text != "hi" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestTranscribeServiceDurationBackstop(t *testing.T) {
	s := NewTranscribeService(&fakeSTT{out: Transcript{Text: "x", Duration: 61500 * time.Millisecond}}, 0, logging.Discard())
	if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); err != nil {
		t.Fatalf("within grace: %v", err)
	}
	s = NewTranscribeService(&fakeSTT{out: Transcript{Text: "x", Duration: 90 * time.Second}}, 0, logging.Discard())
	if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); !errors.Is(err, model.ErrValidation) {
		t.Fatalf("over cap: %v", err)
	}
}

func TestTranscribeServiceTimeoutAndErrors(t *testing.T) {
	s := NewTranscribeService(&fakeSTT{delay: time.Second}, 20*time.Millisecond, logging.Discard())
	if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); !errors.Is(err, model.ErrTranscriptionUnav) {
		t.Fatalf("timeout: %v", err)
	}
	s = NewTranscribeService(&fakeSTT{err: io.ErrUnexpectedEOF}, 0, logging.Discard())
	if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); !errors.Is(err, model.ErrTranscriptionUnav) {
		t.Fatalf("raw error: %v", err)
	}
	s = NewTranscribeService(&fakeSTT{err: model.New(model.CodeValidationFailed, "bad")}, 0, logging.Discard())
	if _, err := s.Transcribe(context.Background(), TranscribeInput{Audio: webmMagic}); !errors.Is(err, model.ErrValidation) {
		t.Fatalf("domain error passthrough: %v", err)
	}
}
