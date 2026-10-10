package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
)

const (
	// multipartOverhead allows boundaries, part headers and the locale field on top
	// of the audio cap.
	multipartOverhead = 64 << 10
	maxLocaleBytes    = 16
)

// TranscribeHandler serves POST /api/chat/transcribe (docs/03 §7.2, docs/15 §4).
type TranscribeHandler struct{ svc *ai.TranscribeService }

// NewTranscribeHandler builds the handler.
func NewTranscribeHandler(s *ai.TranscribeService) *TranscribeHandler {
	return &TranscribeHandler{svc: s}
}

func badMultipart() error {
	return model.New(model.CodeValidationFailed, "Expected multipart/form-data with an audio file.")
}

// Transcribe streams the multipart body in memory (never spilling parts to temp
// files, so audio is not persisted — docs/15 §7) and returns {text, language}.
func (h *TranscribeHandler) Transcribe(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ai.MaxAudioBytes+multipartOverhead)
	mr, err := c.Request.MultipartReader()
	if err != nil {
		RespondError(c, badMultipart())
		return
	}
	var in ai.TranscribeInput
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			RespondError(c, tooLargeOr(err, badMultipart()))
			return
		}
		switch part.FormName() {
		case "audio":
			b, err := io.ReadAll(io.LimitReader(part, ai.MaxAudioBytes+1))
			if err != nil {
				RespondError(c, tooLargeOr(err, badMultipart()))
				return
			}
			if len(b) > ai.MaxAudioBytes {
				RespondError(c, ai.ErrAudioTooLarge)
				return
			}
			in.Audio, in.Filename = b, part.FileName()
		case "locale":
			b, err := io.ReadAll(io.LimitReader(part, maxLocaleBytes))
			if err != nil {
				RespondError(c, tooLargeOr(err, badMultipart()))
				return
			}
			in.Locale = string(b)
		}
		_ = part.Close()
	}
	out, err := h.svc.Transcribe(c.Request.Context(), in)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func tooLargeOr(err, fallback error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return ai.ErrAudioTooLarge
	}
	return fallback
}
