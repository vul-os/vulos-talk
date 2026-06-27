package meet

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRoomNameDeterministicAndOpaque(t *testing.T) {
	a := RoomName("channel-123")
	b := RoomName("channel-123")
	if a != b {
		t.Fatalf("room name not deterministic: %q != %q", a, b)
	}
	if a == RoomName("channel-456") {
		t.Fatal("distinct channels must derive distinct rooms")
	}
	if !strings.HasPrefix(a, "talk-") {
		t.Fatalf("expected talk- prefix, got %q", a)
	}
	if strings.Contains(a, "channel-123") {
		t.Fatalf("room name must not leak the channel id: %q", a)
	}
}

func TestModeAndEnabled(t *testing.T) {
	cases := []struct {
		name        string
		cfg         Config
		wantMode    string
		wantEnabled bool
	}{
		{"unconfigured", Config{}, "", false},
		{"local-missing-url", Config{APIKey: "k", APISecret: "s"}, "local", false},
		{"local-ok", Config{MeetURL: "https://meet", APIKey: "k", APISecret: "s"}, "local", true},
		{"cp", Config{CPBaseURL: "https://cp"}, "cp", true},
		{"cp-wins-over-local", Config{CPBaseURL: "https://cp", APIKey: "k", APISecret: "s"}, "cp", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Mode(); got != tc.wantMode {
				t.Fatalf("Mode()=%q want %q", got, tc.wantMode)
			}
			if got := tc.cfg.Enabled(); got != tc.wantEnabled {
				t.Fatalf("Enabled()=%v want %v", got, tc.wantEnabled)
			}
		})
	}
}

func TestMintJoinDisabled(t *testing.T) {
	_, err := Config{}.MintJoin(context.Background(), "c1", "u1")
	if err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestMintLocalClaimShape(t *testing.T) {
	cfg := Config{
		MeetURL:   "https://meet.example",
		APIKey:    "APIabc",
		APISecret: "supersecretsupersecret",
		Tenant:    "acme",
		Sep:       ":",
		TTL:       time.Hour,
	}
	join, err := cfg.MintJoin(context.Background(), "general", "u_42")
	if err != nil {
		t.Fatal(err)
	}
	if join.MeetURL != "https://meet.example" {
		t.Fatalf("meet url %q", join.MeetURL)
	}
	wantRoom := "acme:" + RoomName("general")
	if join.Room != wantRoom {
		t.Fatalf("room %q want %q", join.Room, wantRoom)
	}

	// Verify signature with the api secret and pin HS256 (alg-confusion guard).
	parsed, err := jwt.Parse(join.Token, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Fatalf("expected HMAC signing, got %v", tok.Header["alg"])
		}
		return []byte(cfg.APISecret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("token did not verify: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["iss"] != "APIabc" {
		t.Fatalf("iss=%v", claims["iss"])
	}
	if claims["sub"] != "u_42" || claims["identity"] != "u_42" {
		t.Fatalf("sub/identity=%v/%v", claims["sub"], claims["identity"])
	}
	if claims["name"] != "acme" {
		t.Fatalf("name (tenant audience)=%v", claims["name"])
	}
	video, ok := claims["video"].(map[string]interface{})
	if !ok {
		t.Fatalf("video grant missing: %T", claims["video"])
	}
	if video["room"] != wantRoom {
		t.Fatalf("video.room=%v want %v", video["room"], wantRoom)
	}
	if video["roomJoin"] != true {
		t.Fatalf("video.roomJoin must be true, got %v", video["roomJoin"])
	}
	// Tenant-binding invariant: name == prefix(video.room).
	prefix := video["room"].(string)[:strings.IndexByte(video["room"].(string), ':')]
	if prefix != claims["name"] {
		t.Fatalf("tenant binding broken: prefix %q != name %q", prefix, claims["name"])
	}
}

func TestMintLocalRejectsTenantWithSeparator(t *testing.T) {
	cfg := Config{MeetURL: "https://m", APIKey: "k", APISecret: "s", Tenant: "a:b", Sep: ":"}
	if _, err := cfg.MintJoin(context.Background(), "c", "u"); err != ErrInvalidTenant {
		t.Fatalf("want ErrInvalidTenant, got %v", err)
	}
}

func TestBrokerViaCP(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get(HeaderRelayAuth)
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "brokered.jwt.token",
			"meet_url":   "https://meet.cloud",
			"room_id":    "acme:talk-deadbeef",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	cfg := Config{CPBaseURL: srv.URL, CPToken: "svc-token", Tenant: "acme", Sep: ":", TTL: time.Hour}
	join, err := cfg.MintJoin(context.Background(), "general", "u_7")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/meet/token" {
		t.Fatalf("path %q", gotPath)
	}
	if gotAuth != "svc-token" {
		t.Fatalf("X-Relay-Auth header %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"tenant":"acme"`) || !strings.Contains(gotBody, `"user_id":"u_7"`) {
		t.Fatalf("broker request body missing tenant/user: %s", gotBody)
	}
	if join.Token != "brokered.jwt.token" {
		t.Fatalf("token=%q", join.Token)
	}
	if join.MeetURL != "https://meet.cloud" {
		t.Fatalf("meet url=%q", join.MeetURL)
	}
	if join.Room != "acme:talk-deadbeef" {
		t.Fatalf("room=%q", join.Room)
	}
}

func TestBrokerViaCPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	cfg := Config{CPBaseURL: srv.URL, Tenant: "acme", Sep: ":"}
	if _, err := cfg.MintJoin(context.Background(), "c", "u"); err == nil {
		t.Fatal("expected error on non-200 cp response")
	}
}
