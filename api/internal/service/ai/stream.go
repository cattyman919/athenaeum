package ai

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// SSE event names (docs/06 §6). Order within one turn is fixed:
// meta → (tool_call → tool_result)* → token* → citations → done; error replaces the rest.
const (
	EventMeta       = "meta"
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventToken      = "token"
	EventCitations  = "citations"
	EventDone       = "done"
	EventError      = "error"
)

// Finish reasons carried by the done event (docs/06 §6).
const (
	FinishStop             = "stop"
	FinishRateLimitedDaily = "rate_limited_daily"
)

// Event is one SSE event; Data is JSON-encoded by the writer.
type Event struct {
	Name string
	Data any
}

// Sink receives the events of a turn (the handler's SSE writer). A Send error means
// the client is gone and the turn is abandoned.
type Sink interface {
	Send(Event) error
}

// MetaData is the meta payload.
type MetaData struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Turn           int       `json:"turn"`
	Model          string    `json:"model"`
	Locale         string    `json:"locale"`
}

// ToolCallData is the tool_call payload.
type ToolCallData struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResultData is the tool_result payload.
type ToolResultData struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// TokenData is the token payload.
type TokenData struct {
	Text string `json:"text"`
}

// Citation is one citation card.
type Citation struct {
	BookID  uuid.UUID `json:"book_id"`
	Title   string    `json:"title"`
	Path    *string   `json:"path"`
	Snippet string    `json:"snippet"`
	Score   float64   `json:"score"`
}

// CitationsData is the citations payload.
type CitationsData struct {
	Citations []Citation `json:"citations"`
}

// DoneData is the done payload.
type DoneData struct {
	MessageID    uuid.UUID `json:"message_id"`
	TokensIn     int       `json:"tokens_in"`
	TokensOut    int       `json:"tokens_out"`
	FinishReason string    `json:"finish_reason"`
}

// ErrorData is the error payload.
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var markerRe = regexp.MustCompile(`\[doc#(\d+)\]`)

// maxMarkerLen bounds how long a pending "[doc#…" prefix is held back.
const maxMarkerLen = len("[doc#000]")

// markerFilter strips [doc#N] citation markers from a streamed answer while recording
// the referenced N in first-mention order. Text that might start a marker is held back
// until it is resolved, so markers split across deltas are still removed.
type markerFilter struct {
	pending strings.Builder
	cited   []int
	seen    map[int]bool
	out     strings.Builder // full filtered answer
}

func newMarkerFilter() *markerFilter { return &markerFilter{seen: map[int]bool{}} }

// Push consumes one delta and returns the text safe to emit now.
func (f *markerFilter) Push(delta string) string {
	f.pending.WriteString(delta)
	buf := f.pending.String()
	f.pending.Reset()

	var emit strings.Builder
	for {
		i := strings.IndexByte(buf, '[')
		if i < 0 {
			emit.WriteString(buf)
			buf = ""
			break
		}
		emit.WriteString(buf[:i])
		buf = buf[i:]
		if loc := markerRe.FindStringSubmatchIndex(buf); loc != nil && loc[0] == 0 {
			n, _ := strconv.Atoi(buf[loc[2]:loc[3]])
			if !f.seen[n] {
				f.seen[n] = true
				f.cited = append(f.cited, n)
			}
			buf = buf[loc[1]:]
			continue
		}
		if couldBeMarker(buf) {
			break // hold back until more text arrives
		}
		emit.WriteByte('[')
		buf = buf[1:]
	}
	f.pending.WriteString(buf)
	s := emit.String()
	f.out.WriteString(s)
	return s
}

// Flush releases any held-back text at the end of the stream.
func (f *markerFilter) Flush() string {
	s := f.pending.String()
	f.pending.Reset()
	f.out.WriteString(s)
	return s
}

// Text is the full filtered answer emitted so far.
func (f *markerFilter) Text() string { return f.out.String() }

// Cited returns the referenced finding numbers in first-mention order.
func (f *markerFilter) Cited() []int { return f.cited }

// couldBeMarker reports whether s (starting with '[') is a proper prefix of a marker.
func couldBeMarker(s string) bool {
	if len(s) >= maxMarkerLen+1 {
		return false
	}
	const lit = "[doc#"
	if len(s) <= len(lit) {
		return strings.HasPrefix(lit, s)
	}
	if !strings.HasPrefix(s, lit) {
		return false
	}
	for _, r := range s[len(lit):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
