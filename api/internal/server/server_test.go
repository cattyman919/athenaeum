package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/testutil/memauth"
)

func TestLoginIsRateLimitedAfterFiveAttempts(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	svc := auth.New(memauth.NewUsers(), memauth.NewTokens(), jm, nil, time.Hour, logging.Discard())
	r := server.NewRouter(server.Deps{
		Config: config.Config{}, Log: logging.Discard(), JWT: jm, Auth: svc,
		Limiter: middleware.NewRateLimiter(rdb, logging.Discard()),
	})

	login := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"email":"x@athenaeum.dev","password":"wrong-wrong-wrong"}`))
		req.RemoteAddr = "10.1.1.1:5555"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for i := 1; i <= 5; i++ {
		if rec := login(); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := login()
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "RATE_LIMITED") {
		t.Fatalf("6th attempt: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}

	// The general bucket does not count login (specific bucket shadows it).
	if v, _ := mr.Get("rl:general:ip:" + ""); v != "" {
		t.Fatal("unexpected general key")
	}
	for _, k := range mr.Keys() {
		if strings.HasPrefix(k, "rl:general") {
			t.Fatalf("general bucket applied to login: %s", k)
		}
	}
}
