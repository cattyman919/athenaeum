// Package index builds the pgvector index: chunking, embedding and storage
// orchestration for cmd/reindex (docs/06 §2, docs/08 §7).
package index

import (
	"strings"
	"unicode"
)

// ChunkerVersion is stored in book_embeddings.chunker_version.
const ChunkerVersion = "v1"

const (
	chunkTokens   = 300
	overlapTokens = 50
)

// approxTokens estimates tokens as ~4 characters each (good enough for sizing).
func approxTokens(s string) int { return (len(s) + 3) / 4 }

// splitSentences splits on sentence-final punctuation followed by whitespace.
func splitSentences(text string) []string {
	text = strings.Join(strings.Fields(text), " ")
	var out []string
	start := 0
	rs := []rune(text)
	for i, r := range rs {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(rs) || unicode.IsSpace(rs[i+1])) {
			if s := strings.TrimSpace(string(rs[start : i+1])); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if tail := strings.TrimSpace(string(rs[start:])); tail != "" {
		out = append(out, tail)
	}
	return out
}

// Chunk implements chunker v1: whole sentences packed up to ~300 tokens with
// ~50 tokens of trailing-sentence overlap between consecutive chunks.
// A single sentence longer than the budget becomes its own chunk.
func Chunk(text string) []string {
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return nil
	}
	var chunks []string
	var cur []string
	curTokens := 0
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, strings.Join(cur, " "))
		}
	}
	for _, s := range sentences {
		t := approxTokens(s)
		if curTokens+t > chunkTokens && len(cur) > 0 {
			flush()
			// Start the next chunk with the trailing sentences worth ~overlapTokens.
			var carry []string
			carried := 0
			for j := len(cur) - 1; j >= 0 && carried < overlapTokens; j-- {
				carry = append([]string{cur[j]}, carry...)
				carried += approxTokens(cur[j])
			}
			// Never let the carried overlap crowd out new content.
			if carried+t > chunkTokens {
				carry, carried = nil, 0
			}
			cur, curTokens = carry, carried
		}
		cur = append(cur, s)
		curTokens += t
	}
	// Avoid emitting a final chunk that is purely overlap with the previous one.
	if len(chunks) > 0 && len(cur) > 0 && strings.HasSuffix(chunks[len(chunks)-1], strings.Join(cur, " ")) {
		return chunks
	}
	flush()
	return chunks
}
