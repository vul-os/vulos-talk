package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"vulos-talk/backend/middleware"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newWriteReadRouter builds a minimal router that has:
//   - POST /write — protected by the given rate-limit middleware
//   - GET /read  — no rate limiting at all
func newWriteReadRouter(store *middleware.BucketStore, keyFn middleware.KeyFunc) *gin.Engine {
	r := gin.New()
	r.POST("/write", middleware.RateLimit(store, keyFn), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/read", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// hit sends a single request to the router, faking the given remote IP.
func hit(r *gin.Engine, method, path, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	// Gin reads ClientIP from RemoteAddr when no trusted proxy headers are set.
	req.RemoteAddr = ip + ":1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --------------------------------------------------------------------------
// Unit tests: Bucket and BucketStore
// --------------------------------------------------------------------------

// TestBucketReserveAllow verifies that a bucket allows exactly burst requests
// and denies the (burst+1)th.
func TestBucketReserveAllow(t *testing.T) {
	const burst = 3
	store := middleware.NewBucketStore(1.0, burst, time.Minute)
	bucket := store.Get("key1")

	for i := 0; i < burst; i++ {
		ok, _ := bucket.Reserve()
		if !ok {
			t.Fatalf("request %d/%d should be allowed (burst=%d)", i+1, burst, burst)
		}
	}

	ok, retryAfter := bucket.Reserve()
	if ok {
		t.Fatal("request beyond burst should be denied")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter should be > 0 when throttled, got %v", retryAfter)
	}
}

// TestBucketRetryAfterIsPositiveSeconds verifies the retryAfter value is at
// least 1 second and is a whole number of seconds.
func TestBucketRetryAfterIsPositiveSeconds(t *testing.T) {
	store := middleware.NewBucketStore(0.5, 1, time.Minute) // 1 token every 2s
	bucket := store.Get("retry-test")

	bucket.Reserve() // consume the 1 burst token
	ok, retryAfter := bucket.Reserve()
	if ok {
		t.Fatal("should be throttled")
	}
	if retryAfter < time.Second {
		t.Errorf("retryAfter should be >= 1s, got %v", retryAfter)
	}
	if retryAfter%time.Second != 0 {
		t.Errorf("retryAfter should be a whole number of seconds, got %v", retryAfter)
	}
}

// TestBucketStoreIsolation verifies that two different keys maintain
// completely independent token counts.
func TestBucketStoreIsolation(t *testing.T) {
	store := middleware.NewBucketStore(1.0, 2, time.Minute)

	b1 := store.Get("alice")
	b2 := store.Get("bob")

	// Exhaust alice's bucket.
	b1.Reserve()
	b1.Reserve()
	ok1, _ := b1.Reserve()
	if ok1 {
		t.Fatal("alice's bucket should be exhausted after 2+1 requests")
	}

	// Bob's bucket should still be at full capacity.
	ok2, _ := b2.Reserve()
	if !ok2 {
		t.Fatal("bob's bucket is independent and should allow requests")
	}
}

// TestBucketRefillOverTime verifies that tokens actually replenish.
func TestBucketRefillOverTime(t *testing.T) {
	// 4 tokens/sec, burst=2: exhaust, wait 300ms → ~1.2 tokens should refill.
	store := middleware.NewBucketStore(4.0, 2, time.Minute)
	bucket := store.Get("refill-key")

	bucket.Reserve()
	bucket.Reserve()
	ok, _ := bucket.Reserve()
	if ok {
		t.Fatal("bucket should be empty after burst")
	}

	time.Sleep(300 * time.Millisecond)

	ok, _ = bucket.Reserve()
	if !ok {
		t.Fatal("after 300ms at 4 tokens/s, at least 1 token should have refilled")
	}
}

// --------------------------------------------------------------------------
// Integration tests: RateLimit middleware via Gin httptest
// --------------------------------------------------------------------------

// TestRateLimitMiddlewareAllows confirms that requests within the burst
// all receive HTTP 200.
func TestRateLimitMiddlewareAllows(t *testing.T) {
	const burst = 5
	store := middleware.NewBucketStore(10.0, burst, time.Minute)
	r := newWriteReadRouter(store, middleware.IPKey)

	for i := 0; i < burst; i++ {
		w := hit(r, "POST", "/write", "192.0.2.1")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d/%d: expected 200, got %d", i+1, burst, w.Code)
		}
	}
}

// TestRateLimitMiddleware429 confirms that the (burst+1)th request receives
// HTTP 429 with a valid Retry-After header and a JSON error body.
func TestRateLimitMiddleware429(t *testing.T) {
	const burst = 3
	store := middleware.NewBucketStore(0.1, burst, time.Minute) // very slow refill
	r := newWriteReadRouter(store, middleware.IPKey)
	ip := "192.0.2.2"

	for i := 0; i < burst; i++ {
		w := hit(r, "POST", "/write", ip)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should be 200, got %d", i+1, w.Code)
		}
	}

	w := hit(r, "POST", "/write", ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	// Retry-After header must be present and a positive integer.
	ra := w.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("Retry-After header must be present on 429 response")
	}
	secs, err := strconv.Atoi(ra)
	if err != nil {
		t.Fatalf("Retry-After must be an integer, got %q: %v", ra, err)
	}
	if secs <= 0 {
		t.Errorf("Retry-After must be positive, got %d", secs)
	}

	// Response body must be valid JSON with an "error" field.
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["error"] == nil {
		t.Error("response body must contain an 'error' field")
	}
	if body["retry_after"] == nil {
		t.Error("response body must contain a 'retry_after' field")
	}
}

// TestRateLimitMiddlewarePerUserIsolation verifies that exhausting one user's
// bucket does not affect another user's quota (UserOrIPKey).
func TestRateLimitMiddlewarePerUserIsolation(t *testing.T) {
	const burst = 2
	store := middleware.NewBucketStore(0.1, burst, time.Minute)

	// Router that simulates the Auth middleware by reading X-User-ID into the
	// context before the rate-limit middleware runs.
	r := gin.New()
	r.POST("/write",
		func(c *gin.Context) {
			if uid := c.GetHeader("X-User-ID"); uid != "" {
				c.Set(middleware.CtxUserID, uid)
			}
			c.Next()
		},
		middleware.RateLimit(store, middleware.UserOrIPKey),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		},
	)

	doAs := func(userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/write", nil)
		req.Header.Set("X-User-ID", userID)
		req.RemoteAddr = "192.0.2.10:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Exhaust alice's bucket.
	for i := 0; i < burst; i++ {
		doAs("alice")
	}
	w := doAs("alice")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("alice should be throttled after %d requests, got %d", burst, w.Code)
	}

	// Bob is on a separate bucket — he should still be allowed.
	w = doAs("bob")
	if w.Code != http.StatusOK {
		t.Fatalf("bob should be unaffected by alice's exhaustion, got %d", w.Code)
	}
}

// TestReadRouteNotRateLimited proves that read endpoints without the
// middleware are never throttled, even when the write quota is exhausted.
func TestReadRouteNotRateLimited(t *testing.T) {
	const burst = 1
	store := middleware.NewBucketStore(0.1, burst, time.Minute)
	r := newWriteReadRouter(store, middleware.IPKey)
	ip := "192.0.2.3"

	// Exhaust the write bucket.
	hit(r, "POST", "/write", ip)
	w := hit(r, "POST", "/write", ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatal("write bucket should be exhausted")
	}

	// Read route must be unaffected — fire many requests.
	for i := 0; i < 10; i++ {
		w := hit(r, "GET", "/read", ip)
		if w.Code != http.StatusOK {
			t.Fatalf("read request %d should be 200 (no rate limit applied), got %d", i+1, w.Code)
		}
	}
}

// TestIPKeyDifferentIPsIsolated verifies that different IPs have independent
// buckets when using IPKey.
func TestIPKeyDifferentIPsIsolated(t *testing.T) {
	const burst = 1
	store := middleware.NewBucketStore(0.1, burst, time.Minute)
	r := newWriteReadRouter(store, middleware.IPKey)

	// IP A exhausts its quota.
	hit(r, "POST", "/write", "10.0.0.1")
	w := hit(r, "POST", "/write", "10.0.0.1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatal("10.0.0.1 should be throttled")
	}

	// IP B is unaffected.
	w = hit(r, "POST", "/write", "10.0.0.2")
	if w.Code != http.StatusOK {
		t.Fatalf("10.0.0.2 should be allowed, got %d", w.Code)
	}
}
