package index_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/index"
)

type fakeStore struct {
	books    []model.IndexBook
	replaced []model.ChunkRow
	ids      []string
	writes   int
	stale    int
}

func (f *fakeStore) Books(_ context.Context, id string) ([]model.IndexBook, error) {
	if id == "" {
		return f.books, nil
	}
	for _, b := range f.books {
		if b.ID == id {
			return []model.IndexBook{b}, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) Replace(_ context.Context, ids []string, rows []model.ChunkRow) error {
	f.writes++
	f.ids, f.replaced = ids, rows
	return nil
}

func (f *fakeStore) StaleBooks(context.Context, string, string) ([]model.StaleBook, error) {
	return []model.StaleBook{{ID: "x", Title: "Old", Chunks: 2}}, nil
}

func (f *fakeStore) StaleChunkCount(context.Context, string, string) (int, error) {
	return f.stale, nil
}

type fakeEmb struct {
	calls []int
	fail  int // fail the Nth call (1-based); 0 = never
}

func (e *fakeEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, len(texts))
	if e.fail != 0 && len(e.calls) == e.fail {
		return nil, model.New(model.CodeEmbeddingUnavailable, "down")
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{float32(len(texts[i]))}
	}
	return out, nil
}

func books(n int) []model.IndexBook {
	var bs []model.IndexBook
	for i := 0; i < n; i++ {
		bs = append(bs, model.IndexBook{ID: string(rune('a' + i)), Title: "T" + string(rune('A'+i)),
			Authors: []string{"Au"}, Description: "One sentence about book."})
	}
	return bs
}

func TestRunBatchesBySizeEightAndWritesOnce(t *testing.T) {
	st := &fakeStore{books: books(20)}
	emb := &fakeEmb{}
	sum, err := index.New(st, emb, "nomic-embed-text", logging.Discard()).Run(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := emb.calls; len(got) != 3 || got[0] != 8 || got[1] != 8 || got[2] != 4 {
		t.Fatalf("batches %v", got)
	}
	if sum.IndexedBooks != 20 || sum.ChunksWritten != 20 || st.writes != 1 || len(st.ids) != 20 {
		t.Fatalf("%+v writes=%d", sum, st.writes)
	}
	r := st.replaced[0]
	if r.Model != "nomic-embed-text" || r.ChunkerVersion != "v1" || r.ChunkIndex != 0 || r.Content != "One sentence about book." {
		t.Fatalf("row %+v", r)
	}
}

func TestRunEmbedderFailureWritesNothing(t *testing.T) {
	st := &fakeStore{books: books(20)}
	_, err := index.New(st, &fakeEmb{fail: 2}, "m", logging.Discard()).Run(context.Background(), "")
	var de *model.Error
	if !errors.As(err, &de) || de.Code != model.CodeEmbeddingUnavailable {
		t.Fatalf("got %v", err)
	}
	if st.writes != 0 {
		t.Fatal("no partial writes allowed (docs/08 §8)")
	}
}

func TestRunBookWithoutDescriptionPrunesRows(t *testing.T) {
	st := &fakeStore{books: []model.IndexBook{{ID: "z", Title: "Empty"}}}
	sum, err := index.New(st, &fakeEmb{}, "m", logging.Discard()).Run(context.Background(), "z")
	if err != nil || sum.ChunksWritten != 0 || st.writes != 1 || len(st.ids) != 1 {
		t.Fatalf("%+v %v writes=%d", sum, err, st.writes)
	}
}

func TestRunEmbedsTitleAuthorsContext(t *testing.T) {
	var texts []string
	emb := captureEmb{out: &texts}
	st := &fakeStore{books: books(1)}
	if _, err := index.New(st, emb, "m", logging.Discard()).Run(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "TA by Au. ") {
		t.Fatalf("%q", texts)
	}
}

type captureEmb struct{ out *[]string }

func (c captureEmb) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	*c.out = append(*c.out, texts...)
	return make([][]float32, len(texts)), nil
}

func TestModelCheck(t *testing.T) {
	got, err := index.New(&fakeStore{}, &fakeEmb{}, "m", logging.Discard()).ModelCheck(context.Background())
	if err != nil || len(got) != 1 || got[0].Title != "Old" {
		t.Fatalf("%v %v", got, err)
	}
}
