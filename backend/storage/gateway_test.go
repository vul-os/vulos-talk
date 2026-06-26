package storage

import (
	"net/http"
	"testing"
)

// brokeredHeader returns a header set that passes the broker-auth gate. Tests
// must also call t.Setenv(EnvStorageBrokerSecret, testBrokerSecret) so the gate
// is open. The endpoint defaults to a safe https endpoint.
const testBrokerSecret = "test-broker-secret"

func brokeredHeader() http.Header {
	h := http.Header{}
	h.Set(HdrStorageBrokerAuth, testBrokerSecret)
	h.Set(HdrStorageEndpoint, "https://s3.example.com")
	return h
}

func TestNewGatewayS3Client_AbsentEndpoint(t *testing.T) {
	t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)

	// Broker auth present but no storage-seam endpoint ⇒ not behind the gateway.
	h := http.Header{}
	h.Set(HdrStorageBrokerAuth, testBrokerSecret)
	if _, ok := NewGatewayS3Client(h); ok {
		t.Fatal("expected ok=false when X-Vulos-Storage-Endpoint is absent")
	}
	// Empty endpoint header is treated the same as absent.
	h.Set(HdrStorageEndpoint, "")
	if _, ok := NewGatewayS3Client(h); ok {
		t.Fatal("expected ok=false when X-Vulos-Storage-Endpoint is empty")
	}
}

// The broker-auth gate must close (seam headers ignored) when the env secret is
// unset, the auth header is missing, or the auth header does not match — even
// when a full, well-formed set of storage-seam headers is present.
func TestNewGatewayS3Client_BrokerAuthGate(t *testing.T) {
	full := func() http.Header {
		h := http.Header{}
		h.Set(HdrStorageEndpoint, "https://s3.example.com")
		h.Set(HdrStorageBucket, "user-bucket")
		h.Set(HdrStoragePrefix, "users/u123/")
		h.Set(HdrStorageAccessKey, "AK")
		h.Set(HdrStorageSecretKey, "SK")
		return h
	}

	t.Run("env secret unset ⇒ headers ignored", func(t *testing.T) {
		// EnvStorageBrokerSecret deliberately not set.
		h := full()
		h.Set(HdrStorageBrokerAuth, "anything")
		if _, ok := NewGatewayS3Client(h); ok {
			t.Fatal("expected ok=false when broker secret env is unset — seam credential trust leak")
		}
	})

	t.Run("auth header missing ⇒ headers ignored", func(t *testing.T) {
		t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)
		h := full() // no HdrStorageBrokerAuth
		if _, ok := NewGatewayS3Client(h); ok {
			t.Fatal("expected ok=false when X-Vulos-Storage-Broker-Auth is absent")
		}
	})

	t.Run("auth header wrong ⇒ headers ignored", func(t *testing.T) {
		t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)
		h := full()
		h.Set(HdrStorageBrokerAuth, "wrong-secret")
		if _, ok := NewGatewayS3Client(h); ok {
			t.Fatal("expected ok=false when X-Vulos-Storage-Broker-Auth mismatches — seam credential trust leak")
		}
	})

	t.Run("auth header matches ⇒ headers honored", func(t *testing.T) {
		t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)
		h := full()
		h.Set(HdrStorageBrokerAuth, testBrokerSecret)
		c, ok := NewGatewayS3Client(h)
		if !ok {
			t.Fatal("expected ok=true when broker auth matches")
		}
		if c.bucket != "user-bucket" {
			t.Fatalf("bucket mismatch: %q", c.bucket)
		}
	})
}

// Endpoint safety: plaintext http is honored only for loopback/private hosts;
// public http endpoints are rejected even with valid broker auth.
func TestNewGatewayS3Client_EndpointSafety(t *testing.T) {
	t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)

	cases := []struct {
		endpoint string
		want     bool
	}{
		{"https://s3.example.com", true},        // https public — ok
		{"https://10.0.0.5:9000", true},         // https private — ok
		{"http://127.0.0.1:9000", true},         // loopback http — ok (on-box MinIO)
		{"http://localhost:9000", true},         // localhost http — ok
		{"http://10.1.2.3:9000", true},          // RFC-1918 http — ok
		{"http://192.168.1.10:9000", true},      // RFC-1918 http — ok
		{"http://minio:9000", true},             // single-label service host http — ok
		{"http://s3.amazonaws.com", false},      // public http — rejected
		{"http://evil.example.com:9000", false}, // public http — rejected
		{"ftp://127.0.0.1", false},              // non-http(s) scheme — rejected
	}
	for _, tc := range cases {
		h := http.Header{}
		h.Set(HdrStorageBrokerAuth, testBrokerSecret)
		h.Set(HdrStorageEndpoint, tc.endpoint)
		_, ok := NewGatewayS3Client(h)
		if ok != tc.want {
			t.Errorf("endpoint %q: got ok=%v want %v", tc.endpoint, ok, tc.want)
		}
	}
}

func TestNewGatewayS3Client_CarvesTalkPrefix(t *testing.T) {
	t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)

	h := http.Header{}
	h.Set(HdrStorageBrokerAuth, testBrokerSecret)
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
	t.Setenv(EnvStorageBrokerSecret, testBrokerSecret)

	h := brokeredHeader()
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
