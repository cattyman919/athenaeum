package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/handler"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
)

func limiterRouter(t *testing.T, b middleware.Bucket, mr *miniredis.Miniredis) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	l := middleware.NewRateLimiter(rdb, logging.Discard())
	r := gin.New()
	r.GET("/x", l.Limit(b, handler.RespondError, nil), func(c *gin.Context) { c.Status(200) })
	r.GET("/skipped", l.Limit(b, handler.RespondError, func(*gin.Context) bool { return true }), func(c *gin.Context) { c.Status(200) })
	return r
}

func hit(r http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/x", nil)
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestLimiterEnforcesBucketAndHeaders(t *testing.T) {
	mr := miniredis.RunT(t)
	b := middleware.Bucket{Name: "t", Limit: 3, Window: time.Minute}
	r := limiterRouter(t, b, mr)

	for i := 1; i <= 3; i++ {
		rec := hit(r, "10.0.0.1")
		if rec.Code != 200 {
			t.Fatalf("req %d: %d", i, rec.Code)
		}
		if rec.Header().Get("X-RateLimit-Limit") != "3" ||
			rec.Header().Get("X-RateLimit-Remaining") != strconv.Itoa(3-i) ||
			rec.Header().Get("X-RateLimit-Reset") == "" {
			t.Fatalf("headers %v", rec.Header())
		}
	}
	rec := hit(r, "10.0.0.1")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("breach: %d %v", rec.Code, rec.Header())
	}
	if rec := hit(r, "10.0.0.2"); rec.Code != 200 {
		t.Fatalf("other peer must have own counter: %d", rec.Code)
	}

	// Window expiry resets the counter.
	mr.FastForward(2 * time.Minute)
	if rec := hit(r, "10.0.0.1"); rec.Code != 200 {
		t.Fatalf("after window: %d", rec.Code)
	}
}

func TestLimiterSkipAndFailOpen(t *testing.T) {
	mr := miniredis.RunT(t)
	b := middleware.Bucket{Name: "t", Limit: 1, Window: time.Minute}
	r := limiterRouter(t, b, mr)
	for range 3 {
		req := httptest.NewRequest("GET", "/skipped", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("skip: %d", rec.Code)
		}
	}
	mr.Close() // Redis down → fail open.
	if rec := hit(r, "10.0.0.9"); rec.Code != 200 {
		t.Fatalf("fail open: %d", rec.Code)
	}
}

func TestBucketTableMatchesDocs(t *testing.T) {
	tests := []struct {
		b      middleware.Bucket
		limit  int
		window time.Duration
	}{
		{middleware.BucketLogin, 5, 15 * time.Minute},
		{middleware.BucketRegister, 5, 15 * time.Minute},
		{middleware.BucketGeneral, 120, time.Minute},
		{middleware.BucketSearch, 30, time.Minute},
		{middleware.BucketChatMin, 5, time.Minute},
		{middleware.BucketChatDay, 50, 24 * time.Hour},
		{middleware.BucketTranscribe, 10, time.Minute},
		{middleware.BucketTranscribePartial, 60, time.Minute},
		{middleware.BucketFetchISBN, 10, time.Minute},
	}
	for _, tt := range tests {
		if tt.b.Limit != tt.limit || tt.b.Window != tt.window {
			t.Errorf("%s: %d/%v", tt.b.Name, tt.b.Limit, tt.b.Window)
		}
	}
}

func TestSingleFlightAllowsOneInFlightPerPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	l := middleware.NewRateLimiter(rdb, logging.Discard())
	entered, release := make(chan struct{}), make(chan struct{})
	r := gin.New()
	r.GET("/x", l.SingleFlight("t", time.Minute, handler.RespondError, nil), func(c *gin.Context) {
		if c.Query("block") == "1" {
			entered <- struct{}{}
			<-release
		}
		c.Status(200)
	})
	get := func(ip, q string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/x"+q, nil)
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- get("10.0.0.1", "?block=1") }()
	<-entered
	if rec := get("10.0.0.1", ""); rec.Code != 429 || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrent same peer: %d %v", rec.Code, rec.Header())
	}
	if rec := get("10.0.0.2", ""); rec.Code != 200 {
		t.Fatalf("other peer must not be blocked: %d", rec.Code)
	}
	close(release)
	if rec := <-done; rec.Code != 200 {
		t.Fatalf("holder: %d", rec.Code)
	}
	if rec := get("10.0.0.1", ""); rec.Code != 200 {
		t.Fatalf("lock must be released after the holder returns: %d", rec.Code)
	}
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("leftover locks %v", keys)
	}

	// Redis down → fail open.
	mr.Close()
	if rec := get("10.0.0.1", ""); rec.Code != 200 {
		t.Fatalf("fail open: %d", rec.Code)
	}
}
