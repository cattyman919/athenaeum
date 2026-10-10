package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/handler"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/server"
)

func do(t *testing.T, h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	r := server.NewRouter(server.Deps{Config: config.Config{WebOrigin: "http://localhost:3000"}, Log: logging.Discard()})
	rec := do(t, r, http.MethodGet, "/api/health", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != "v1" {
		t.Fatalf("body %v", body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	r := server.NewRouter(server.Deps{Log: logging.Discard()})
	rec := do(t, r, http.MethodGet, "/api/health", map[string]string{"X-Request-Id": "abc-123"})
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Fatalf("got %q", got)
	}
}

func TestNotFoundEnvelope(t *testing.T) {
	r := server.NewRouter(server.Deps{Log: logging.Discard()})
	rec := do(t, r, http.MethodGet, "/api/nope", nil)
	if rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "NOT_FOUND" {
		t.Fatalf("body %v", body)
	}
}

func TestCORS(t *testing.T) {
	r := server.NewRouter(server.Deps{Config: config.Config{WebOrigin: "http://localhost:3000"}, Log: logging.Discard()})
	rec := do(t, r, http.MethodOptions, "/api/health", map[string]string{"Origin": "http://localhost:3000"})
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	rec = do(t, r, http.MethodGet, "/api/health", map[string]string{"Origin": "http://evil.example"})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin must not be allowed")
	}
}

func TestErrorMapperCatalog(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{model.New(model.CodeValidationFailed, "bad"), 400, "VALIDATION_FAILED"},
		{model.New(model.CodeInvalidISBN, "bad"), 400, "INVALID_ISBN"},
		{model.New(model.CodeUnauthenticated, "x"), 401, "UNAUTHENTICATED"},
		{model.New(model.CodeReusedToken, "x"), 401, "REUSED_TOKEN"},
		{model.New(model.CodeInvalidCredentials, "x"), 401, "INVALID_CREDENTIALS"},
		{model.New(model.CodeForbidden, "x"), 403, "FORBIDDEN"},
		{model.New(model.CodeNotFound, "x"), 404, "NOT_FOUND"},
		{model.New(model.CodeConflict, "x"), 409, "CONFLICT"},
		{model.New(model.CodeEmailTaken, "x"), 409, "EMAIL_TAKEN"},
		{model.New(model.CodeISBNConflict, "x"), 409, "ISBN_CONFLICT"},
		{model.New(model.CodeBookNotAvailable, "x"), 409, "BOOK_NOT_AVAILABLE"},
		{model.New(model.CodeRateLimited, "x"), 429, "RATE_LIMITED"},
		{model.New(model.CodeUpstreamError, "x"), 502, "UPSTREAM_ERROR"},
		{model.New(model.CodeEmbeddingUnavailable, "x"), 503, "EMBEDDING_UNAVAILABLE"},
		{model.New(model.CodeTranscriptionUnavailable, "x"), 503, "TRANSCRIPTION_UNAVAILABLE"},
		{errors.New("boom"), 500, "UNEXPECTED"},
	}
	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			r := gin.New()
			r.GET("/x", func(c *gin.Context) { handler.RespondError(c, tt.err) })
			rec := do(t, r, http.MethodGet, "/x", nil)
			if rec.Code != tt.status {
				t.Fatalf("status %d want %d", rec.Code, tt.status)
			}
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["error"] != tt.code || body["message"] == "" {
				t.Fatalf("body %v", body)
			}
		})
	}
}

func TestErrorMapperExtraKeysAndNoLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		handler.RespondError(c, model.ErrBookUnavailable.WithExtra(map[string]any{"suggested_actions": []string{"reserve"}}))
	})
	r.GET("/y", func(c *gin.Context) {
		handler.RespondError(c, model.Wrap(model.CodeUpstreamError, "down", errors.New("secret-internal")))
	})
	rec := do(t, r, http.MethodGet, "/x", nil)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["suggested_actions"] == nil {
		t.Fatalf("missing extra keys: %v", body)
	}
	rec = do(t, r, http.MethodGet, "/y", nil)
	if got := rec.Body.String(); strings.Contains(got, "secret-internal") {
		t.Fatalf("cause leaked: %s", got)
	}
}

func TestErrorsIsByCode(t *testing.T) {
	if !errors.Is(model.New(model.CodeNotFound, "other msg"), model.ErrNotFound) {
		t.Fatal("errors.Is should match by code")
	}
}
