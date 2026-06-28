package middleware

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Bucket is a single token-bucket rate-limit counter.
// Tokens refill continuously at refillRate tokens/second up to maxTokens.
type Bucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// Reserve consumes one token if available.
// Returns (true, 0) when the request is allowed.
// Returns (false, d) when throttled; d is the duration until retry is safe.
func (b *Bucket) Reserve() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = math.Min(b.maxTokens, b.tokens+elapsed*b.refillRate)
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Seconds until 1 token is available, rounded up to the nearest second.
	secs := math.Ceil((1 - b.tokens) / b.refillRate)
	return false, time.Duration(secs) * time.Second
}

// bucketEntry wraps a Bucket with a last-seen timestamp for idle eviction.
type bucketEntry struct {
	bucket   *Bucket
	lastSeen time.Time
}

// BucketStore manages per-key token buckets with automatic eviction of idle entries.
type BucketStore struct {
	mu      sync.Mutex
	entries map[string]*bucketEntry

	rate  float64       // refill rate: tokens per second
	burst float64       // maximum burst (and initial token count)
	ttl   time.Duration // idle eviction window
}

// NewBucketStore creates a store where each bucket allows `burst` requests
// immediately, then refills at `ratePerSec` tokens per second.
// Buckets idle for longer than `ttl` are evicted automatically.
func NewBucketStore(ratePerSec float64, burst int, ttl time.Duration) *BucketStore {
	s := &BucketStore{
		entries: make(map[string]*bucketEntry),
		rate:    ratePerSec,
		burst:   float64(burst),
		ttl:     ttl,
	}
	go s.evictLoop()
	return s
}

// Get returns (or lazily creates) the Bucket for the given key.
func (s *BucketStore) Get(key string) *Bucket {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		e = &bucketEntry{
			bucket: &Bucket{
				tokens:     s.burst,
				maxTokens:  s.burst,
				refillRate: s.rate,
				lastRefill: time.Now(),
			},
		}
		s.entries[key] = e
	}
	e.lastSeen = time.Now()
	return e.bucket
}

// Len returns the number of actively tracked buckets (useful in tests).
func (s *BucketStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *BucketStore) evictLoop() {
	ticker := time.NewTicker(s.ttl / 2)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-s.ttl)
		s.mu.Lock()
		for k, e := range s.entries {
			if e.lastSeen.Before(cutoff) {
				delete(s.entries, k)
			}
		}
		s.mu.Unlock()
	}
}

// KeyFunc extracts the rate-limit bucket key from a gin context.
// Returning the same key for two requests makes them share a bucket.
type KeyFunc func(c *gin.Context) string

// UserOrIPKey uses the authenticated user ID when present (set by Auth
// middleware via CtxUserID), otherwise falls back to the client IP.
// Use this for endpoints behind the Auth middleware.
func UserOrIPKey(c *gin.Context) string {
	if uid := c.GetString(CtxUserID); uid != "" {
		return "user:" + uid
	}
	return "ip:" + c.ClientIP()
}

// IPKey always keys on the client IP address.
// Use this for unauthenticated endpoints (e.g. incoming webhooks).
func IPKey(c *gin.Context) string {
	return "ip:" + c.ClientIP()
}

// BotTokenKey uses the first 16 characters of the Bearer token as the key so
// that each bot has its own bucket regardless of which IP it sends from.
// Falls back to client IP when no Bearer token is present.
func BotTokenKey(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	const pfx = "Bearer "
	if strings.HasPrefix(auth, pfx) {
		tok := auth[len(pfx):]
		if len(tok) > 16 {
			tok = tok[:16]
		}
		if tok != "" {
			return "bot:" + tok
		}
	}
	return "ip:" + c.ClientIP()
}

// RateLimit returns a Gin middleware that applies per-key token-bucket
// throttling using the given store and key function.
//
// Allowed requests pass through to the next handler.
// Throttled requests are aborted with HTTP 429 Too Many Requests and a
// Retry-After header indicating the number of seconds until retry is safe.
func RateLimit(store *BucketStore, keyFn KeyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFn(c)
		bucket := store.Get(key)
		allowed, retryAfter := bucket.Reserve()
		if !allowed {
			secs := int(math.Ceil(retryAfter.Seconds()))
			c.Header("Retry-After", fmt.Sprintf("%d", secs))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": secs,
			})
			return
		}
		c.Next()
	}
}
