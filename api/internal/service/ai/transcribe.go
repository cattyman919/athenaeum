package ai

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Voice-input limits (docs/15 §6).
const (
	MaxAudioBytes     = 10 << 20         // ~10 MB upload cap
	MaxClipDuration   = 60 * time.Second // client-enforced; server backstop below
	TranscribeTimeout = 30 * time.Second // upstream Whisper deadline
	// clipDurationGrace absorbs MediaRecorder stop latency over the 60 s client cap.
	clipDurationGrace = 2 * time.Second
)

// TranscribeInput is one push-to-talk clip. Audio lives only in memory for the
// duration of the request (docs/15 §7).
type TranscribeInput struct {
	Audio    []byte
	Filename string
	// Locale is the UI locale (en | zh-CN); anything else → Whisper auto-detect.
	Locale string
}

// Transcript is the response body of POST /api/chat/transcribe (docs/03 §7.2).
type Transcript struct {
	Text     string        `json:"text"`
	Language string        `json:"language"`
	Duration time.Duration `json:"-"`
}

// Transcriber is the STT port; WhisperAdapter (providers.go) is the production ACL.
type Transcriber interface {
	Transcribe(ctx context.Context, in TranscribeInput) (Transcript, error)
}

// TranscribeService validates clips and calls the STT adapter under the 30 s deadline.
type TranscribeService struct {
	stt     Transcriber
	timeout time.Duration
	log     *slog.Logger
}

// NewTranscribeService builds the service; timeout <= 0 uses TranscribeTimeout.
func NewTranscribeService(stt Transcriber, timeout time.Duration, log *slog.Logger) *TranscribeService {
	if timeout <= 0 {
		timeout = TranscribeTimeout
	}
	return &TranscribeService{stt: stt, timeout: timeout, log: log}
}

func invalidAudio(msg string) error { return model.New(model.CodeValidationFailed, msg) }

// ErrAudioTooLarge is returned for uploads over MaxAudioBytes.
var ErrAudioTooLarge = invalidAudio("The recording is too large (max 10 MB).")

// Transcribe validates the clip (presence, size, obviously non-audio payloads), calls
// the adapter and enforces the duration backstop. Logs carry sizes and durations
// only — never audio bytes or transcript text.
func (s *TranscribeService) Transcribe(ctx context.Context, in TranscribeInput) (Transcript, error) {
	switch {
	case len(in.Audio) == 0:
		return Transcript{}, invalidAudio("An audio recording is required.")
	case len(in.Audio) > MaxAudioBytes:
		return Transcript{}, ErrAudioTooLarge
	case strings.HasPrefix(http.DetectContentType(in.Audio), "text/"):
		return Transcript{}, invalidAudio("The audio could not be decoded.")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	start := time.Now()
	out, err := s.stt.Transcribe(ctx, in)
	elapsed := time.Since(start)
	if err != nil {
		if _, ok := model.AsError(err); !ok || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = model.Wrap(model.CodeTranscriptionUnavailable, "Voice transcription is temporarily unavailable.", err)
		}
		s.log.WarnContext(ctx, "transcription failed", "bytes", len(in.Audio), "duration_ms", elapsed.Milliseconds())
		return Transcript{}, err
	}
	if out.Duration > MaxClipDuration+clipDurationGrace {
		return Transcript{}, invalidAudio("The recording is too long (max 60 seconds).")
	}
	s.log.InfoContext(ctx, "transcribed", "bytes", len(in.Audio), "clip_ms", out.Duration.Milliseconds(),
		"duration_ms", elapsed.Milliseconds(), "language", out.Language, "text_len", len([]rune(out.Text)))
	return out, nil
}
