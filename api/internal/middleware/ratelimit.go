package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Bucket is one rate-limit rule from docs/03 §2.
type Bucket struct {
	Name   string
	Limit  int
	Window time.Duration
	// ByUser keys by user id (falling back to hashed IP when anonymous);
	// otherwise the key is always the hashed client IP.
	ByUser bool
}

// Buckets from docs/03 §2.
var (
	BucketLogin      = Bucket{Name: "login", Limit: 5, Window: 15 * time.Minute}
	BucketRegister   = Bucket{Name: "register", Limit: 5, Window: 15 * time.Minute}
	BucketGeneral    = Bucket{Name: "general", Limit: 120, Window: time.Minute, ByUser: true}
	BucketSearch     = Bucket{Name: "search", Limit: 30, Window: time.Minute, ByUser: true}
	BucketChatMin    = Bucket{Name: "chat_min", Limit: 5, Window: time.Minute, ByUser: true}
	BucketChatDay    = Bucket{Name: "chat_day", Limit: 50, Window: 24 * time.Hour, ByUser: true}
	BucketTranscribe = Bucket{Name: "transcribe", Limit: 10, Window: time.Minute, ByUser: true}
	// BucketTranscribePartial governs live (interim) transcription calls
	// (POST /api/chat/transcribe?partial=true, docs/15 §6); it never shares the
	// 10/min final-transcript bucket.
	BucketTranscribePartial = Bucket{Name: "transcribe_partial", Limit: 60, Window: time.Minute, ByUser: true}
	BucketFetchISBN         = Bucket{Name: "fetch_isbn", Limit: 10, Window: time.Minute, ByUser: true}
	// BucketScan governs patron ISBN / copy-tag lookups (POST /api/books/scan).
	BucketScan = Bucket{Name: "books_scan", Limit: 30, Window: time.Minute, ByUser: true}
	// BucketCopyReturn governs patron self-service returns (POST /api/copies/{id}/return).
	BucketCopyReturn = Bucket{Name: "copy_return", Limit: 10, Window: time.Minute, ByUser: true}
)

// RateLimiter applies Redis fixed-window counters (INCR + EXPIRE).
type RateLimiter struct {
	rdb redis.Cmdable
	log *slog.Logger
	now func() time.Time
}

// NewRateLimiter builds a limiter over any go-redis client.
func NewRateLimiter(rdb redis.Cmdable, log *slog.Logger) *RateLimiter {
	return &RateLimiter{rdb: rdb, log: log, now: time.Now}
}

func peerKey(c *gin.Context, b Bucket) string {
	if b.ByUser {
		if id, ok := UserID(c); ok {
			return "u:" + id.String()
		}
	}
	sum := sha256.Sum256([]byte(c.ClientIP()))
	return "ip:" + hex.EncodeToString(sum[:8])
}

// Limit returns middleware enforcing b; skip (optional) exempts requests that a
// more specific bucket governs. Redis failures fail open with a warning.
func (l *RateLimiter) Limit(b Bucket, respond ErrorResponder, skip func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if skip != nil && skip(c) {
			c.Next()
			return
		}
		key := fmt.Sprintf("rl:%s:%s", b.Name, peerKey(c, b))
		ctx := c.Request.Context()
		count, ttl, err := l.hit(ctx, key, b.Window)
		if err != nil {
			l.log.Warn("rate limiter unavailable; failing open", "bucket", b.Name, "err", err)
			c.Next()
			return
		}
		remaining := b.Limit - int(count)
		if remaining < 0 {
			remaining = 0
		}
		reset := l.now().Add(ttl).Unix()
		h := c.Writer.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(b.Limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		if int(count) > b.Limit {
			secs := int(ttl.Seconds())
			if secs < 1 {
				secs = 1
			}
			h.Set("Retry-After", strconv.Itoa(secs))
			respond(c, model.New(model.CodeRateLimited, "Too many requests. Please try again later."))
			return
		}
		c.Next()
	}
}

func (l *RateLimiter) hit(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	ttlCmd := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}
	ttl := ttlCmd.Val()
	if ttl < 0 {
		ttl = window
	}
	return incr.Val(), ttl, nil
}

// releaseLock deletes an in-flight lock only while it still holds our token, so a
// request that outlived the TTL never frees a newer request's lock.
var releaseLock = redis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

// SingleFlight allows at most one in-flight request per peer (user id, else hashed
// IP) under name; a concurrent second request gets 429 RATE_LIMITED with
// Retry-After: 1. The Redis lock (SET NX PX ttl) is released when the handler
// returns; ttl only bounds a crashed holder. skip (optional) exempts requests.
// Redis failures fail open with a warning, like Limit.
func (l *RateLimiter) SingleFlight(name string, ttl time.Duration, respond ErrorResponder, skip func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if skip != nil && skip(c) {
			c.Next()
			return
		}
		key := fmt.Sprintf("inflight:%s:%s", name, peerKey(c, Bucket{ByUser: true}))
		token := uuid.NewString()
		ctx := c.Request.Context()
		ok, err := l.rdb.SetNX(ctx, key, token, ttl).Result()
		if err != nil {
			l.log.Warn("in-flight guard unavailable; failing open", "guard", name, "err", err)
			c.Next()
			return
		}
		if !ok {
			c.Header("Retry-After", "1")
			respond(c, model.New(model.CodeRateLimited, "Another request of this kind is still in progress."))
			return
		}
		defer func() {
			if err := releaseLock.Run(context.WithoutCancel(ctx), l.rdb, []string{key}, token).Err(); err != nil {
				l.log.Warn("in-flight guard release failed", "guard", name, "err", err)
			}
		}()
		c.Next()
	}
}
