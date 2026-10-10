package embed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cattyman919/athenaeum/api/internal/embed"
	"github.com/cattyman919/athenaeum/api/internal/model"
)

func fakeOllama(t *testing.T, dim int, status int) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req.Input...)
		if r.URL.Path != "/api/embed" || req.Model != "nomic-embed-text" {
			t.Errorf("unexpected request %s %+v", r.URL.Path, req)
		}
		if status != 200 {
			http.Error(w, "model not found", status)
			return
		}
		out := make([][]float32, len(req.Input))
		for i := range out {
			out[i] = make([]float32, dim)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestEmbedDocumentsAndQueryPrefixes(t *testing.T) {
	srv, seen := fakeOllama(t, 768, 200)
	o := embed.NewOllama(srv.URL, "nomic-embed-text", 768)
	vs, err := o.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil || len(vs) != 2 || len(vs[0]) != 768 {
		t.Fatalf("%v %v", len(vs), err)
	}
	if _, err := o.EmbedQuery(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*seen, "|") != "search_document: a|search_document: b|search_query: q" {
		t.Fatalf("inputs %q", *seen)
	}
}

func TestEmbedErrorsMapToEmbeddingUnavailable(t *testing.T) {
	cases := map[string]*embed.Ollama{}
	srvErr, _ := fakeOllama(t, 768, 500)
	cases["http 500"] = embed.NewOllama(srvErr.URL, "nomic-embed-text", 768)
	srvDim, _ := fakeOllama(t, 384, 200)
	cases["wrong dim"] = embed.NewOllama(srvDim.URL, "nomic-embed-text", 768)
	cases["connection refused"] = embed.NewOllama("http://127.0.0.1:1", "nomic-embed-text", 768)
	for name, o := range cases {
		_, err := o.EmbedDocuments(context.Background(), []string{"x"})
		de, ok := model.AsError(err)
		if !ok || de.Code != model.CodeEmbeddingUnavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	o := embed.NewOllama("http://127.0.0.1:1", "m", 4)
	if vs, err := o.EmbedDocuments(context.Background(), nil); err != nil || vs != nil {
		t.Fatalf("%v %v", vs, err)
	}
}
