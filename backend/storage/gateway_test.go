package storage

import (
	"net/http"
	"testing"
)

func TestNewGatewayS3Client_AbsentEndpoint(t *testing.T) {
	// No storage-seam headers ⇒ not behind the gateway.
	if _, ok := NewGatewayS3Client(http.Header{}); ok {
		t.Fatal("expected ok=false when X-Vulos-Storage-Endpoint is absent")
	}
	// Empty endpoint header is treated the same as absent.
	h := http.Header{}
	h.Set(HdrStorageEndpoint, "")
	if _, ok := NewGatewayS3Client(h); ok {
		t.Fatal("expected ok=false when X-Vulos-Storage-Endpoint is empty")
	}
}

func TestNewGatewayS3Client_CarvesTalkPrefix(t *testing.T) {
	h := http.Header{}
	h.Set(HdrStorageEndpoint, "https://s3.example.com/")
	h.Set(HdrStorageBucket, "user-bucket")
	h.Set(HdrStoragePrefix, "users/u123/")
	h.Set(HdrStorageRegion, "us-east-1")
	h.Set(HdrStorageAccessKey, "AK")
	h.Set(HdrStorageSecretKey, "SK")
	h.Set(HdrStorageSession, "TOKEN")

	c, ok := NewGatewayS3Client(h)
	if !ok {
		t.Fatal("expected ok=true when endpoint header is present")
	}
	if c.bucket != "user-bucket" || c.region != "us-east-1" {
		t.Fatalf("bucket/region mismatch: %q %q", c.bucket, c.region)
	}
	if c.endpoint != "https://s3.example.com" {
		t.Fatalf("endpoint trailing slash not trimmed: %q", c.endpoint)
	}
	if c.sessionToken != "TOKEN" {
		t.Fatalf("session token not captured: %q", c.sessionToken)
	}
	// Objects live under "<injected-prefix>/talk/...".
	got := c.key(TalkScopedName("acct1", "rec.webm"))
	want := "users/u123/talk/acct1/rec.webm"
	if got != want {
		t.Fatalf("key mismatch: got %q want %q", got, want)
	}
}

func TestNewGatewayS3Client_DefaultPrefixAndRegion(t *testing.T) {
	h := http.Header{}
	h.Set(HdrStorageEndpoint, "https://s3.example.com")
	c, ok := NewGatewayS3Client(h)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if c.region != "auto" {
		t.Fatalf("expected default region 'auto', got %q", c.region)
	}
	if got, want := c.key("x"), "talk/x"; got != want {
		t.Fatalf("default prefix: got %q want %q", got, want)
	}
}

func TestTalkScopedName_Sanitizes(t *testing.T) {
	// A caller-controlled account/name cannot escape Talk's key space.
	if got := TalkScopedName("../../etc", "a/b"); got != "____etc/a_b" {
		t.Fatalf("sanitize mismatch: %q", got)
	}
	// Empty account omits the account segment.
	if got := TalkScopedName("", "rec.webm"); got != "rec.webm" {
		t.Fatalf("empty account: %q", got)
	}
}

func TestSignV4_IncludesSessionToken(t *testing.T) {
	c := &OfficeS3Client{
		endpoint:        "https://s3.example.com",
		region:          "auto",
		bucket:          "b",
		accessKeyID:     "AK",
		secretAccessKey: "SK",
		sessionToken:    "SESSION",
		httpClient:      http.DefaultClient,
	}
	req, err := c.signed(http.MethodPut, "talk/x", []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Amz-Security-Token") != "SESSION" {
		t.Fatal("expected X-Amz-Security-Token header to be set")
	}
	if auth := req.Header.Get("Authorization"); auth == "" ||
		!contains(auth, "x-amz-security-token") {
		t.Fatalf("session token not in SignedHeaders: %q", auth)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
