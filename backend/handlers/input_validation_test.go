package handlers

// input_validation_test.go — input-validation / injection hardening at the REST
// boundary: malformed JSON, oversized bodies (DoS bound), and the storage-safety
// contract for hostile message bodies.
//
// Storage-safety contract: the server stores message bodies VERBATIM (it is a
// transport, not a renderer) and returns them as properly JSON-escaped values.
// XSS defence lives at the render boundary (DOMPurify in the React client — see
// src/lib/sanitize.js and the frontend pentest suites). These tests prove the
// server (a) does not crash or mangle hostile input, and (b) never emits a body
// that breaks out of the JSON string context.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"

	"github.com/gin-gonic/gin"
)

// rawReq serves a request with a caller-supplied raw body (so we can send
// deliberately malformed JSON the typed helpers would never produce).
func rawReq(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSendMessage_MalformedJSON400(t *testing.T) {
	h := testHandler(t)
	r := router(h, "alice", false) // router() wires POST .../messages
	for _, bad := range []string{`{`, `{"body":}`, `not json at all`, `["array"]`, ``} {
		w := rawReq(r, http.MethodPost, "/spaces/channels/general/messages", bad)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("malformed body %q: expected 400, got %d (%s)", bad, w.Code, w.Body.String())
		}
	}
}

func TestSendMessage_OversizedBodyRejected(t *testing.T) {
	h := testHandler(t)
	r := router(h, "alice", false)
	huge := strings.Repeat("A", spaces.MaxMessageBytes+1)
	w := doReq(r, http.MethodPost, "/spaces/channels/general/messages",
		models.SendMessageRequest{Body: huge})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized message: expected 400 (DoS bound), got %d", w.Code)
	}
	// Nothing oversized landed in the channel.
	for _, m := range h.store.ListMessages("general") {
		if len(m.Body) > spaces.MaxMessageBytes {
			t.Fatal("oversized message persisted despite rejection")
		}
	}
}

// TestSendMessage_HostileBodyStoredVerbatimAndJSONSafe sends a battery of XSS /
// injection payloads and asserts (a) they are accepted (server is a transport),
// (b) stored byte-for-byte, and (c) the JSON response round-trips to the exact
// same string — i.e. the payload never escapes the JSON string context.
func TestSendMessage_HostileBodyStoredVerbatimAndJSONSafe(t *testing.T) {
	payloads := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`"><svg/onload=alert(1)>`,
		`javascript:alert(document.cookie)`,
		`{{constructor.constructor('alert(1)')()}}`,
		"line1\nline2\twith\ttabs",
		`Robert'); DROP TABLE messages;--`,
		"emoji 🔥 and unicode ☃ ZWSP​",
	}
	for _, payload := range payloads {
		h := testHandler(t)
		r := router(h, "alice", false)
		w := doReq(r, http.MethodPost, "/spaces/channels/general/messages",
			models.SendMessageRequest{Body: payload})
		if w.Code != http.StatusCreated {
			t.Fatalf("payload %q: expected 201, got %d (%s)", payload, w.Code, w.Body.String())
		}
		var msg models.Message
		mustDecode(t, w, &msg)
		if msg.Body != payload {
			t.Fatalf("payload mangled by server: stored %q, sent %q", msg.Body, payload)
		}
		// The stored copy matches too.
		stored, _ := h.store.GetMessage("general", msg.ID)
		if stored.Body != payload {
			t.Fatalf("stored body differs from input: %q vs %q", stored.Body, payload)
		}
		// The raw response must be valid JSON (no broken-out string context).
		var probe map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
			t.Fatalf("response is not valid JSON for payload %q: %v", payload, err)
		}
	}
}

// TestCreateChannel_MalformedJSON400 covers the channel-create binding path.
func TestCreateChannel_MalformedJSON400(t *testing.T) {
	h := testHandler(t)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, "alice")
		c.Next()
	})
	r.POST("/spaces/channels", h.CreateChannel)
	if w := rawReq(r, http.MethodPost, "/spaces/channels", `{"name":`); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed channel body, got %d", w.Code)
	}
}
