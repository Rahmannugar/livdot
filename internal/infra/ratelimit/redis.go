// Package ratelimit provides Redis-backed distributed rate limiting.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// token bucket smooths bursts; the sliding window counter enforces the quota
// over the whole window. A request must pass both.
var (
	tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local bucket = redis.call("hmget", key, "tokens", "ts")
local tokens = tonumber(bucket[1])
local ts = tonumber(bucket[2])
if tokens == nil then tokens = capacity; ts = now end
local delta = math.max(0, now - ts) / 1000
tokens = math.min(capacity, tokens + delta * refill)
local allowed = 0
if tokens >= 1 then tokens = tokens - 1; allowed = 1 end
redis.call("hset", key, "tokens", tokens, "ts", now)
redis.call("pexpire", key, ttl)
return {allowed, math.floor(tokens)}
`)

	slidingWindowScript = redis.NewScript(`
local current = KEYS[1]
local previous = KEYS[2]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local currentCount = tonumber(redis.call("get", current) or "0")
local previousCount = tonumber(redis.call("get", previous) or "0")
local elapsed = now % window
local weight = (window - elapsed) / window
local estimate = currentCount + previousCount * weight
local allowed = 0
if estimate < limit then
  currentCount = redis.call("incr", current)
  if currentCount == 1 then redis.call("pexpire", current, window * 2) end
  allowed = 1
end
local remaining = math.floor(limit - estimate)
if remaining < 0 then remaining = 0 end
return {allowed, remaining}
`)
)

// SubjectFunc returns the rate-limit subject for a request, or "" to fall back
// to the client IP. Wiring supplies this so ratelimit stays auth-agnostic.
type SubjectFunc func(ctx *gin.Context) string

type Limiter struct {
	client  *redis.Client
	prefix  string
	subject SubjectFunc
	now     func() time.Time
}

func New(client *redis.Client, prefix string, subject SubjectFunc) (*Limiter, error) {
	if client == nil {
		return nil, fmt.Errorf("rate limit redis client is required")
	}
	if prefix == "" {
		return nil, fmt.Errorf("rate limit key prefix is required")
	}
	if subject == nil {
		subject = func(*gin.Context) string { return "" }
	}
	return &Limiter{client: client, prefix: prefix, subject: subject, now: time.Now}, nil
}

// Result reports whether the caller may proceed.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int64
	RetryAfter time.Duration
}

// Allow checks the token bucket and the sliding window for one key.
func (limiter *Limiter) Allow(ctx context.Context, key string, policy Policy) (Result, error) {
	now := limiter.now()
	bucket, err := tokenBucketScript.Run(ctx, limiter.client,
		[]string{limiter.prefix + ":tb:" + key},
		policy.Burst,
		policy.RefillPerSecond,
		now.UnixMilli(),
		policy.Window.Milliseconds(),
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate limit token bucket: %w", err)
	}
	window, err := limiter.slidingWindow(ctx, key, policy, now)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Allowed:    bucket[0] == 1 && window[0] == 1,
		Limit:      policy.WindowLimit,
		Remaining:  minInt64(bucket[1], window[1]),
		RetryAfter: policy.Window,
	}, nil
}

func (limiter *Limiter) slidingWindow(ctx context.Context, key string, policy Policy, now time.Time) ([]int64, error) {
	windowMillis := policy.Window.Milliseconds()
	index := now.UnixMilli() / windowMillis
	base := limiter.prefix + ":sw:" + key
	current := fmt.Sprintf("%s:%d", base, index)
	previous := fmt.Sprintf("%s:%d", base, index-1)

	values, err := slidingWindowScript.Run(ctx, limiter.client,
		[]string{current, previous},
		policy.WindowLimit,
		windowMillis,
		now.UnixMilli(),
	).Int64Slice()
	if err != nil {
		return nil, fmt.Errorf("rate limit sliding window: %w", err)
	}
	return values, nil
}

// Middleware limits a route using the supplied policy.
func (limiter *Limiter) Middleware(policy Policy) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		result, err := limiter.Allow(ctx.Request.Context(), limiter.key(ctx, policy), policy)
		if err != nil {
			// fail open: a cache outage must not lock every caller out.
			slog.Warn("rate limiter unavailable; request allowed",
				"policy", policy.Name,
				"route", ctx.FullPath(),
				"error", err,
			)
			ctx.Next()
			return
		}
		ctx.Header("X-RateLimit-Limit", strconv.Itoa(result.Limit))
		ctx.Header("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
		if !result.Allowed {
			ctx.Header("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"code": "rate_limited", "message": "too many requests"},
			})
			return
		}
		ctx.Next()
	}
}

func (limiter *Limiter) key(ctx *gin.Context, policy Policy) string {
	subject := ""
	if policy.KeyBy == KeyByAccount {
		subject = limiter.subject(ctx)
	}
	if subject == "" {
		subject = ctx.ClientIP()
	}
	return policy.Name + "|" + subject + "|" + ctx.FullPath()
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
