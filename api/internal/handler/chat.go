package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
)

// HeartbeatInterval is the SSE keep-alive comment cadence (docs/06 §6).
const HeartbeatInterval = 15 * time.Second

// ChatHandler serves /api/chat (docs/03 §7).
type ChatHandler struct {
	chat      *ai.Service
	heartbeat time.Duration
}

// NewChatHandler builds the handler; heartbeat <= 0 uses HeartbeatInterval.
func NewChatHandler(s *ai.Service, heartbeat time.Duration) *ChatHandler {
	if heartbeat <= 0 {
		heartbeat = HeartbeatInterval
	}
	return &ChatHandler{chat: s, heartbeat: heartbeat}
}

// List handles GET /api/chat/conversations.
func (h *ChatHandler) List(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	page, err := intParam(c, "page")
	if err != nil {
		RespondError(c, err)
		return
	}
	perPage, err := intParam(c, "per_page")
	if err != nil {
		RespondError(c, err)
		return
	}
	res, err := h.chat.List(c.Request.Context(), uid, page, perPage)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Create handles POST /api/chat/conversations (body {title?}, may be empty).
func (h *ChatHandler) Create(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	var req struct {
		Title *string `json:"title"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondError(c, badBody())
			return
		}
	}
	conv, err := h.chat.Create(c.Request.Context(), uid, req.Title)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, conv)
}

// Get handles GET /api/chat/conversations/{id}.
func (h *ChatHandler) Get(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	d, err := h.chat.Get(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

// Delete handles DELETE /api/chat/conversations/{id}.
func (h *ChatHandler) Delete(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	if err := h.chat.Delete(c.Request.Context(), uid, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Stream handles POST /api/chat: errors before the first byte use the JSON envelope;
// afterwards the turn streams SSE events (docs/03 §7.1, docs/06 §6).
func (h *ChatHandler) Stream(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	var req struct {
		ConversationID string `json:"conversation_id"`
		Message        string `json:"message"`
		Locale         string `json:"locale"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, badBody())
		return
	}
	// The 50/24h bucket runs last, so its headers are the ones on the response.
	hdr := c.Writer.Header()
	daily := hdr.Get("X-RateLimit-Limit") == "50" && hdr.Get("X-RateLimit-Remaining") == "0"

	turn, err := h.chat.Prepare(c.Request.Context(), uid, ai.ChatRequest{
		ConversationID: req.ConversationID, Message: req.Message, Locale: req.Locale, DailyQuotaExhausted: daily,
	})
	if err != nil {
		RespondError(c, err)
		return
	}

	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	w := &sseWriter{w: c.Writer}
	w.flush()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(h.heartbeat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := w.comment("ping"); err != nil {
					return
				}
			}
		}
	}()
	turn.Run(c.Request.Context(), w)
	close(stop)
	wg.Wait()
}

// sseWriter serializes SSE frames; safe for concurrent use (heartbeat + events).
type sseWriter struct {
	mu sync.Mutex
	w  gin.ResponseWriter
}

func (s *sseWriter) flush() { s.w.Flush() }

// Send writes "event: <name>\ndata: <json>\n\n".
func (s *sseWriter) Send(e ai.Event) error {
	data, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", e.Name, data); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

func (s *sseWriter) comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}
