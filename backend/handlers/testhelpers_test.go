package handlers

// testhelpers_test.go — shared request/response helpers for the Vulos Talk
// handler test suites (spaces, bots, pentests). Only the helpers the Talk tests
// actually use (doReq, mustDecode) are kept; the office file/signing and the
// removed meeting/recording handler helpers were dropped with those routes.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// doReq serves a JSON request through the router and returns the recorder.
func doReq(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// mustDecode unmarshals a recorder body into v, failing the test on error.
func mustDecode(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
}
