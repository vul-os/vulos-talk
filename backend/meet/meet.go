// Package meet is Talk's seam-C handoff to the dedicated vulos-meet product.
//
// Real-time audio/video is NOT hosted by Talk. When a member starts or joins a
// huddle in a channel, Talk derives a deterministic Meet room from the channel
// id, obtains a short-lived VULOS-MEET/1 join token, and hands the member off to
// the Meet web client (embedded in an iframe). Meet's in-call chat is pointed
// back at the originating Talk channel so the conversation persists.
//
// Two token sources satisfy the same contract (see Config.Mode):
//
//   - LOCAL mint — Talk signs the VULOS-MEET/1 token itself with the shared
//     LiveKit (api_key, api_secret) pair configured against the Meet deployment
//     (VULOS_MEET_API_KEY / VULOS_MEET_API_SECRET). The token bytes are
//     byte-compatible with what vulos-cloud's minter produces and what
//     vulos-meet's wrap.Validator + LiveKit Server verify: HS256 over the same
//     claim set (iss=api_key, sub=identity, name=tenant, video.room=<tenant>:<room>).
//
//   - CP broker — when a vulos-cloud control plane is configured
//     (VULOS_CP_BASE_URL), Talk does NOT hold the api_secret; it asks the control
//     plane to mint (the cloud is the sole token issuer, MEET-CP-01). Talk calls
//     the CP server-to-server with the shared X-Relay-Auth service token, exactly
//     mirroring the integration-seam adapter in backend/integration/cloud.
//
// This package is an OPTIONAL dependency: with no Meet configured, Enabled()
// reports false and the huddle action degrades to a "video not configured"
// state. Talk standalone (chat + Spaces) is fully functional without it — this
// is the seam-C optional cross-product dependency, never a hard one.
package meet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Environment contract.
const (
	// EnvMeetURL is the public base URL of the vulos-meet deployment (the signal
	// gate that also serves the web client, e.g. https://meet.vulos.org). The
	// huddle iframe is deep-linked against this origin.
	EnvMeetURL = "VULOS_MEET_URL"
	// EnvAPIKey / EnvAPISecret are the shared LiveKit credential pair, identical
	// to what the Meet deployment validates with. Required for LOCAL minting.
	EnvAPIKey    = "VULOS_MEET_API_KEY"
	EnvAPISecret = "VULOS_MEET_API_SECRET"
	// EnvTenant is the Vulos tenant audience stamped on every token and used as
	// the room-id prefix. All members of a workspace share one tenant so they
	// land in the same room. Falls back to VULOS_ORG_ID, then "talk".
	EnvTenant = "VULOS_MEET_TENANT"
	// EnvTenantSep overrides the tenant/room separator byte (default ":"). It
	// MUST match the Meet deployment's configured separator.
	EnvTenantSep = "VULOS_MEET_TENANT_SEP"
	// EnvTokenTTL overrides the join-token validity (Go duration, default 2h,
	// hard-capped at 6h per vulos-meet/spec/TOKEN.md §3).
	EnvTokenTTL = "VULOS_MEET_TOKEN_TTL"
	// EnvTalkPublicURL is the public base URL Meet's in-call chat calls back to
	// reach Talk's message API. Falls back to the incoming request's origin.
	EnvTalkPublicURL = "VULOS_TALK_PUBLIC_URL"

	// EnvCPBaseURL / EnvCPToken select + authenticate the CP broker, mirroring
	// backend/integration/cloud (the same control plane, the same service token).
	EnvCPBaseURL = "VULOS_CP_BASE_URL"
	EnvCPToken   = "VULOS_CP_TOKEN"
	EnvOrgID     = "VULOS_ORG_ID"
)

const (
	defaultTenant = "talk"
	defaultSep    = ":"
	defaultTTL    = 2 * time.Hour
	maxTTL        = 6 * time.Hour
	// HeaderRelayAuth is the shared CP authentication header (matches the
	// contract used across Vulos products; the secret is VULOS_CP_TOKEN).
	HeaderRelayAuth = "X-Relay-Auth"
)

// Config holds the resolved seam-C settings. Build it once at startup with
// FromEnv and pass it to the huddle handler.
type Config struct {
	MeetURL   string
	APIKey    string
	APISecret string
	Tenant    string
	Sep       string
	TTL       time.Duration

	TalkPublicURL string

	CPBaseURL string
	CPToken   string

	// httpClient is used for CP brokering; nil falls back to a 5s-timeout client.
	httpClient *http.Client
	// nowFn lets tests inject a deterministic clock.
	nowFn func() time.Time
}

// FromEnv resolves the seam-C config from the environment.
func FromEnv() Config {
	tenant := strings.TrimSpace(os.Getenv(EnvTenant))
	if tenant == "" {
		tenant = strings.TrimSpace(os.Getenv(EnvOrgID))
	}
	if tenant == "" {
		tenant = defaultTenant
	}
	sep := os.Getenv(EnvTenantSep)
	if sep == "" {
		sep = defaultSep
	}
	ttl := defaultTTL
	if raw := strings.TrimSpace(os.Getenv(EnvTokenTTL)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			ttl = d
		}
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	return Config{
		MeetURL:       strings.TrimRight(strings.TrimSpace(os.Getenv(EnvMeetURL)), "/"),
		APIKey:        strings.TrimSpace(os.Getenv(EnvAPIKey)),
		APISecret:     strings.TrimSpace(os.Getenv(EnvAPISecret)),
		Tenant:        tenant,
		Sep:           sep,
		TTL:           ttl,
		TalkPublicURL: strings.TrimRight(strings.TrimSpace(os.Getenv(EnvTalkPublicURL)), "/"),
		CPBaseURL:     strings.TrimRight(strings.TrimSpace(os.Getenv(EnvCPBaseURL)), "/"),
		CPToken:       strings.TrimSpace(os.Getenv(EnvCPToken)),
	}
}

// Mode reports how tokens are obtained: "cp" (brokered by the control plane),
// "local" (signed here with the shared secret), or "" (not configured).
func (c Config) Mode() string {
	switch {
	case c.CPBaseURL != "":
		return "cp"
	case c.APIKey != "" && c.APISecret != "":
		return "local"
	default:
		return ""
	}
}

// Enabled reports whether huddles can be started. In CP mode the control plane
// supplies the Meet URL, so only CPBaseURL is required; in local mode both the
// Meet URL and the signing credentials must be present.
func (c Config) Enabled() bool {
	switch c.Mode() {
	case "cp":
		return true
	case "local":
		return c.MeetURL != ""
	default:
		return false
	}
}

// RoomName derives a stable, opaque per-channel room name. It is deterministic
// (every member of the channel derives the same room) and reveals nothing about
// the channel id. The "talk-" prefix namespaces Talk-originated rooms.
func RoomName(channelID string) string {
	sum := sha256.Sum256([]byte("vulos-talk-huddle:" + channelID))
	return "talk-" + hex.EncodeToString(sum[:8])
}

// QualifiedRoom returns the full <tenant><sep><room> id the token grants and the
// Meet client connects to.
func (c Config) QualifiedRoom(channelID string) string {
	return c.Tenant + c.Sep + RoomName(channelID)
}

// Join is the resolved handoff for a single member joining a channel huddle.
type Join struct {
	MeetURL   string
	Room      string // full <tenant><sep><room> id
	Token     string
	ExpiresAt time.Time
}

// Errors.
var (
	ErrNotConfigured = errors.New("meet: video is not configured")
	ErrInvalidTenant = errors.New("meet: tenant must not contain the separator byte")
)

func (c Config) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c Config) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// MintJoin produces a join handoff for userID in the huddle backing channelID.
// It routes to the CP broker or the local minter per Mode().
func (c Config) MintJoin(ctx context.Context, channelID, userID string) (Join, error) {
	if !c.Enabled() {
		return Join{}, ErrNotConfigured
	}
	if strings.Contains(c.Tenant, c.Sep) {
		return Join{}, ErrInvalidTenant
	}
	room := RoomName(channelID)
	if c.Mode() == "cp" {
		return c.brokerViaCP(ctx, room, userID)
	}
	return c.mintLocal(room, userID)
}

// mintLocal signs a VULOS-MEET/1 token with the shared (api_key, api_secret)
// pair. The produced bytes verify on the Meet side because the claim set is
// identical to what vulos-cloud's minter emits (see package doc).
func (c Config) mintLocal(room, userID string) (Join, error) {
	now := c.now()
	full := c.Tenant + c.Sep + room
	claims := meetClaims{
		Identity: userID,
		Name:     c.Tenant, // tenant audience — must byte-equal the room prefix
		Video: &videoGrant{
			Room:         full,
			RoomJoin:     true,
			CanPublish:   true,
			CanSubscribe: true,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.APIKey,
			Subject:   userID,
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(c.TTL)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(c.APISecret))
	if err != nil {
		return Join{}, fmt.Errorf("meet: sign join token: %w", err)
	}
	return Join{
		MeetURL:   c.MeetURL,
		Room:      full,
		Token:     signed,
		ExpiresAt: now.Add(c.TTL),
	}, nil
}

// brokerViaCP asks the control plane to mint, server-to-server. The control
// plane is the sole token issuer in cloud deployments, so the api_secret never
// lives in Talk. The contract mirrors backend/integration/cloud:
//
//	POST {cp}/api/meet/token         (header X-Relay-Auth: <service token>)
//	  body: {product, account_id, tenant, user_id, room, publish, subscribe}
//	  → {token, meet_url, room_id, expires_at}
//
// `tenant` is sent explicitly so a channel huddle is a shared room across the
// channel's members rather than per-user — an additive, forward-compatible
// extension of the MEET-CP-01 mint route.
func (c Config) brokerViaCP(ctx context.Context, room, userID string) (Join, error) {
	reqBody := cpMintRequest{
		Product:   "talk",
		AccountID: userID,
		Tenant:    c.Tenant,
		UserID:    userID,
		Room:      room,
		Publish:   true,
		Subscribe: true,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return Join{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.CPBaseURL+"/api/meet/token", bytes.NewReader(raw))
	if err != nil {
		return Join{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.CPToken != "" {
		req.Header.Set(HeaderRelayAuth, c.CPToken)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Join{}, fmt.Errorf("meet: cp mint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Join{}, fmt.Errorf("meet: cp mint: status %d", resp.StatusCode)
	}
	var out cpMintResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Join{}, fmt.Errorf("meet: cp mint decode: %w", err)
	}
	if out.Token == "" {
		return Join{}, errors.New("meet: cp mint returned no token")
	}
	meetURL := strings.TrimRight(out.MeetURL, "/")
	if meetURL == "" {
		meetURL = c.MeetURL // fall back to a statically configured URL
	}
	exp := c.now().Add(c.TTL)
	if out.ExpiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, out.ExpiresAt); perr == nil {
			exp = t
		}
	}
	full := out.RoomID
	if full == "" {
		full = c.Tenant + c.Sep + room
	}
	return Join{MeetURL: meetURL, Room: full, Token: out.Token, ExpiresAt: exp}, nil
}

// ── wire shapes ──────────────────────────────────────────────────────────────

// videoGrant is the LiveKit video grant subset Talk emits. The field names match
// livekit/protocol/auth.VideoGrant so vulos-meet (which uses that library to
// verify) unmarshals them identically.
type videoGrant struct {
	Room         string `json:"room"`
	RoomJoin     bool   `json:"roomJoin"`
	CanPublish   bool   `json:"canPublish"`
	CanSubscribe bool   `json:"canSubscribe"`
}

// meetClaims is the VULOS-MEET/1 JWT body: the standard registered claims merged
// with the LiveKit grant claims (identity, name, video), exactly as
// livekit/protocol/auth.AccessToken.ToJWT serialises them.
type meetClaims struct {
	Identity string      `json:"identity,omitempty"`
	Name     string      `json:"name,omitempty"`
	Video    *videoGrant `json:"video,omitempty"`
	jwt.RegisteredClaims
}

type cpMintRequest struct {
	Product   string `json:"product"`
	AccountID string `json:"account_id"`
	Tenant    string `json:"tenant"`
	UserID    string `json:"user_id"`
	Room      string `json:"room"`
	Publish   bool   `json:"publish"`
	Subscribe bool   `json:"subscribe"`
}

type cpMintResponse struct {
	Token     string `json:"token"`
	MeetURL   string `json:"meet_url"`
	RoomID    string `json:"room_id"`
	ExpiresAt string `json:"expires_at"`
}
