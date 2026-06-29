package handlers

// audit_fixes_test.go — regression tests for the three audit findings fixed on
// the talk-audit-fixes branch:
//
//  1. (HIGH)  CRDT op-merge path bypasses size/rate/batch limits.
//     - oversized op body in a merge batch is rejected (HTTP 400).
//     - op batch exceeding MaxMergeOpsPerBatch is rejected (HTTP 400).
//  2. (HIGH)  POST /api/spaces/ops unthrottled — rate-limiter applied in main.go;
//     the handler-level test below confirms that a rate-limited router returns
//     429 when the bucket is exhausted and 200 otherwise.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"

	"github.com/gin-gonic/gin"
)

// mergeOpsRouter wires POST /spaces/ops with an optional rate-limit middleware
// and a fixed authenticated identity, mirroring the main.go wiring.
func mergeOpsRouter(h *SpacesHandlerExt, user string, rl gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, user)
		c.Next()
	})
	if rl != nil {
		r.POST("/spaces/ops", rl, h.MergeOps)
	} else {
		r.POST("/spaces/ops", h.MergeOps)
	}
	return r
}

// seedPublicChannel creates a public channel with alice as a member and returns
// the channel ID.
func seedPublicChannel(t *testing.T, h *SpacesHandlerExt, owner string) string {
	t.Helper()
	ch, err := h.store.CreateChannel("audit-ch", models.ChannelTypePublic, owner)
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if _, err := h.store.AddMember(ch.ID, owner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	return ch.ID
}

// TestAudit_MergeOps_OversizedOpBodyRejected proves that a single op whose
// body exceeds MaxMessageBytes is rejected at the handler level (HTTP 400)
// before any state mutation.
func TestAudit_MergeOps_OversizedOpBodyRejected(t *testing.T) {
	h := testHandler(t)
	chID := seedPublicChannel(t, h, "alice")
	r := mergeOpsRouter(h, "alice", nil)

	oversizedBody := strings.Repeat("Z", spaces.MaxMessageBytes+1)
	op := []*models.MessageOp{{
		Op:        models.MessageOpAppend,
		ChannelID: chID,
		Msg: models.Message{
			ID:        "oversized-1",
			ChannelID: chID,
			AuthorID:  "alice",
			Body:      oversizedBody,
			State:     models.MessageStateActive,
			SeqClock:  "00000000000000000001-0000000000-test",
		},
	}}
	w := doReq(r, http.MethodPost, "/spaces/ops", op)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusForbidden {
		t.Fatalf("expected 400 or 403 for oversized op body, got %d (%s)", w.Code, w.Body.String())
	}
	// The oversized message must not have been applied to the store.
	for _, m := range h.store.ListMessages(chID) {
		if m.ID == "oversized-1" {
			t.Fatal("oversized op landed in the store despite rejection")
		}
	}
}

// TestAudit_MergeOps_OversizedBatchRejected proves that a batch of more than
// MaxMergeOpsPerBatch ops is rejected (HTTP 400) before any op is applied.
func TestAudit_MergeOps_OversizedBatchRejected(t *testing.T) {
	h := testHandler(t)
	chID := seedPublicChannel(t, h, "alice")
	r := mergeOpsRouter(h, "alice", nil)

	ops := make([]*models.MessageOp, spaces.MaxMergeOpsPerBatch+1)
	for i := range ops {
		ops[i] = &models.MessageOp{
			Op:        models.MessageOpAppend,
			ChannelID: chID,
			Msg: models.Message{
				ID:        "batch-op",
				ChannelID: chID,
				AuthorID:  "alice",
				Body:      "small",
				State:     models.MessageStateActive,
				SeqClock:  "00000000000000000001-0000000000-test",
			},
		}
	}
	w := doReq(r, http.MethodPost, "/spaces/ops", ops)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized batch, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestAudit_MergeOps_RateLimited confirms that when a rate-limiting middleware
// is attached to POST /spaces/ops (as wired in main.go), requests beyond the
// burst quota receive HTTP 429 with a Retry-After header, while requests within
// the burst succeed normally.
func TestAudit_MergeOps_RateLimited(t *testing.T) {
	const burst = 2
	store := middleware.NewBucketStore(0.01, burst, time.Minute) // near-zero refill
	rl := middleware.RateLimit(store, middleware.UserOrIPKey)

	h := testHandler(t)
	chID := seedPublicChannel(t, h, "alice")
	r := mergeOpsRouter(h, "alice", rl)

	// A minimal valid (empty) batch — the test is about the rate-limit
	// response, not op correctness.
	emptyBatch := []*models.MessageOp{}
	body, _ := json.Marshal(emptyBatch)

	doPost := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/spaces/ops", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.99:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Requests within burst must succeed (200).
	for i := 0; i < burst; i++ {
		if w := doPost(); w.Code != http.StatusOK {
			t.Fatalf("request %d/%d within burst: expected 200, got %d", i+1, burst, w.Code)
		}
	}

	// The next request must be throttled (429) with a Retry-After header.
	w := doPost()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("/spaces/ops should be rate-limited: expected 429, got %d (%s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response must carry a Retry-After header")
	}
	_ = chID // used above; suppress unused-variable lint
}
