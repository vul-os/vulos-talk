package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"vulos-talk/backend/config"
	"vulos-talk/backend/meet"
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"

	"github.com/gin-gonic/gin"
)

// huddleEnv builds a router wiring the huddle handler over an in-memory spaces
// store, injecting `user` as the verified identity. cfg.Auth.Enabled is false so
// no talk_token is minted (the common self-host case).
func huddleEnv(t *testing.T, mcfg meet.Config, user string) (*gin.Engine, *spaces.SpacesStore) {
	t.Helper()
	store := NewSpacesHandlerWithPersister(spaces.NewNullPersister()).Store()
	h := NewHuddleHandler(store, &config.Config{}, mcfg)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, user)
		c.Next()
	})
	r.GET("/meet/config", h.Config)
	r.POST("/spaces/channels/:channelId/huddle", h.Start)
	return r, store
}

func enabledMeet() meet.Config {
	return meet.Config{
		MeetURL:   "https://meet.example",
		APIKey:    "APIabc",
		APISecret: "supersecretsupersecret",
		Tenant:    "acme",
		Sep:       ":",
		TTL:       time.Hour,
	}
}

func TestHuddleConfig_Disabled(t *testing.T) {
	r, _ := huddleEnv(t, meet.Config{}, "self")
	w := doReq(r, http.MethodGet, "/meet/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	mustDecode(t, w, &resp)
	if resp.Enabled {
		t.Fatal("expected disabled when no Meet configured")
	}
}

func TestHuddleConfig_Enabled(t *testing.T) {
	r, _ := huddleEnv(t, enabledMeet(), "self")
	w := doReq(r, http.MethodGet, "/meet/config", nil)
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	mustDecode(t, w, &resp)
	if !resp.Enabled {
		t.Fatal("expected enabled")
	}
}

func TestHuddleStart_DegradesWhenDisabled(t *testing.T) {
	r, store := huddleEnv(t, meet.Config{}, "self")
	store.CreateChannelWithID("general", "general", models.ChannelTypePublic, "system")
	w := doReq(r, http.MethodPost, "/spaces/channels/general/huddle", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d (expected graceful 200)", w.Code)
	}
	var resp struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	mustDecode(t, w, &resp)
	if resp.Enabled || resp.Reason == "" {
		t.Fatalf("expected enabled=false with a reason, got %+v", resp)
	}
}

func TestHuddleStart_PublicChannelMintsJoin(t *testing.T) {
	r, store := huddleEnv(t, enabledMeet(), "self")
	store.CreateChannelWithID("general", "general", models.ChannelTypePublic, "system")
	w := doReq(r, http.MethodPost, "/spaces/channels/general/huddle", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Enabled     bool   `json:"enabled"`
		JoinURL     string `json:"join_url"`
		Room        string `json:"room"`
		Token       string `json:"token"`
		TalkChannel string `json:"talk_channel"`
		TalkToken   string `json:"talk_token"`
	}
	mustDecode(t, w, &resp)
	if !resp.Enabled || resp.Token == "" {
		t.Fatalf("expected enabled join with token, got %+v", resp)
	}
	if resp.TalkChannel != "general" {
		t.Fatalf("talk_channel=%q", resp.TalkChannel)
	}
	if resp.TalkToken != "" {
		t.Fatal("auth disabled: talk_token should be empty")
	}
	// The deep link must carry the Meet origin, the room, the token, and the Talk
	// chat binding so Meet's in-call chat persists to this channel.
	for _, want := range []string{"https://meet.example/?", "token=", "talkChannel=general", "talkBase="} {
		if !strings.Contains(resp.JoinURL, want) {
			t.Fatalf("join_url missing %q: %s", want, resp.JoinURL)
		}
	}
	wantRoom := "acme:" + meet.RoomName("general")
	if resp.Room != wantRoom {
		t.Fatalf("room=%q want %q", resp.Room, wantRoom)
	}
}

func TestHuddleStart_PrivateChannelRequiresMembership(t *testing.T) {
	r, store := huddleEnv(t, enabledMeet(), "self")
	// Private channel owned by someone else; "self" is not a member.
	store.CreateChannelWithID("secret", "secret", models.ChannelTypePrivate, "bob")
	w := doReq(r, http.MethodPost, "/spaces/channels/secret/huddle", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHuddleStart_UnknownChannel(t *testing.T) {
	r, _ := huddleEnv(t, enabledMeet(), "self")
	w := doReq(r, http.MethodPost, "/spaces/channels/nope/huddle", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHuddleStart_AuthEnabledMintsTalkToken(t *testing.T) {
	t.Setenv(middleware.EnvJWTSecret, "test-secret-value-please-rotate")
	store := NewSpacesHandlerWithPersister(spaces.NewNullPersister()).Store()
	store.CreateChannelWithID("general", "general", models.ChannelTypePublic, "system")
	cfg := &config.Config{}
	cfg.Auth.Enabled = true
	h := NewHuddleHandler(store, cfg, enabledMeet())
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, "alice")
		c.Next()
	})
	r.POST("/spaces/channels/:channelId/huddle", h.Start)

	w := doReq(r, http.MethodPost, "/spaces/channels/general/huddle", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		TalkToken string `json:"talk_token"`
		JoinURL   string `json:"join_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TalkToken == "" {
		t.Fatal("expected a minted talk_token when auth is enabled")
	}
	if !strings.Contains(resp.JoinURL, "talkToken=") {
		t.Fatalf("join_url must carry talkToken: %s", resp.JoinURL)
	}
}
