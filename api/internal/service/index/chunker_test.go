package index

import (
	"fmt"
	"strings"
	"testing"
)

func TestChunkShortDescriptionIsOneChunk(t *testing.T) {
	in := "Having upset the subtle ecology of the desert planet Arrakis, young Paul Atreides is drawn into a struggle over the spice."
	got := Chunk(in)
	if len(got) != 1 || got[0] != in {
		t.Fatalf("%q", got)
	}
}

func TestChunkEmpty(t *testing.T) {
	if Chunk("") != nil || Chunk("   \n ") != nil {
		t.Fatal("empty input must yield no chunks")
	}
}

func longText(sentences int) string {
	var b strings.Builder
	for i := 0; i < sentences; i++ {
		fmt.Fprintf(&b, "Sentence number %d talks about libraries, shelves and the long quiet history of reading rooms. ", i)
	}
	return b.String()
}

func TestChunkSplitsAtSentenceBoundariesWithOverlap(t *testing.T) {
	chunks := Chunk(longText(40)) // ~40*24 tokens ≈ 960 tokens
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if approxTokens(c) > chunkTokens {
			t.Errorf("chunk %d has %d tokens (> %d)", i, approxTokens(c), chunkTokens)
		}
		if !strings.HasSuffix(c, ".") {
			t.Errorf("chunk %d does not end on a sentence boundary: %q", i, c[len(c)-20:])
		}
		if i > 0 {
			// Overlap: the first sentence of this chunk appeared at the end of the previous one.
			first := splitSentences(c)[0]
			if !strings.Contains(chunks[i-1], first) {
				t.Errorf("chunk %d does not overlap chunk %d", i, i-1)
			}
		}
	}
	// Every sentence is covered.
	joined := strings.Join(chunks, " ")
	for i := 0; i < 40; i++ {
		if !strings.Contains(joined, fmt.Sprintf("Sentence number %d ", i)) {
			t.Errorf("sentence %d lost", i)
		}
	}
}

func TestChunkOversizedSingleSentence(t *testing.T) {
	s := strings.Repeat("word ", 400) + "end."
	got := Chunk(s)
	if len(got) != 1 {
		t.Fatalf("got %d chunks", len(got))
	}
}

func TestChunkDeterministic(t *testing.T) {
	a, b := Chunk(longText(30)), Chunk(longText(30))
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("chunking must be deterministic")
	}
}

func TestSplitSentencesKeepsDecimals(t *testing.T) {
	got := splitSentences("Dewey 813.54 is fiction. Really? Yes! Done")
	want := []string{"Dewey 813.54 is fiction.", "Really?", "Yes!", "Done"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", got)
	}
}
