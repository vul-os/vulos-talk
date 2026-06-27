package handlers

import (
	"net/http"
	"net/url"
	"time"

	"vulos-talk/backend/config"
	"vulos-talk/backend/meet"
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// HuddleHandler implements the seam-C handoff: starting/joining a huddle in a
// Talk channel hands the member off to the dedicated vulos-meet product instead
// of Talk hosting its own real-time A/V. Talk derives a deterministic Meet room
// from the channel, obtains a VULOS-MEET/1 join token (locally minted or
// brokered via the control plane — see backend/meet), and returns a deep link
// the SPA embeds in an iframe. Meet's in-call chat is pointed back at the
// originating Talk channel so the conversation persists in Talk.
//
// When Meet is not configured the handler still answers 200 with enabled=false
// so the SPA can degrade the huddle action to a "video not configured" state —
// Talk standalone (chat + Spaces) keeps working with no hard dependency on Meet.
type HuddleHandler struct {
	store *spaces.SpacesStore
	cfg   *config.Config
	meet  meet.Config
}

// NewHuddleHandler builds the handler over the shared spaces store (for channel
// membership authz), the app config (to know whether auth is on), and the
// resolved seam-C Meet config.
func NewHuddleHandler(store *spaces.SpacesStore, cfg *config.Config, mcfg meet.Config) *HuddleHandler {
	return &HuddleHandler{store: store, cfg: cfg, meet: mcfg}
}

// configResponse is the GET /api/meet/config payload the SPA reads on load to
// decide whether to enable the huddle action.
type configResponse struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// Config GET /api/meet/config — reports whether huddles are available.
func (h *HuddleHandler) Config(c *gin.Context) {
	if !h.meet.Enabled() {
		c.JSON(http.StatusOK, configResponse{Enabled: false, Reason: "video not configured"})
		return
	}
	c.JSON(http.StatusOK, configResponse{Enabled: true})
}

// joinResponse is the POST .../huddle payload: everything the SPA needs to embed
// the Meet web client with Talk-backed in-call chat.
type joinResponse struct {
	Enabled     bool   `json:"enabled"`
	Reason      string `json:"reason,omitempty"`
	JoinURL     string `json:"join_url,omitempty"`
	MeetURL     string `json:"meet_url,omitempty"`
	Room        string `json:"room,omitempty"`
	Token       string `json:"token,omitempty"`
	TalkBase    string `json:"talk_base,omitempty"`
	TalkChannel string `json:"talk_channel,omitempty"`
	TalkToken   string `json:"talk_token,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// Start POST /api/spaces/channels/:channelId/huddle — mint a Meet join for the
// caller. Enforces channel membership (private/DM channels require membership;
// public channels are open to any authenticated user, mirroring message authz).
func (h *HuddleHandler) Start(c *gin.Context) {
	channelID := c.Param("channelId")
	requester := requesterID(c)

	// Authorize against the same membership rules as the message API.
	ch, ok := h.store.GetChannel(channelID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	if ch.Type == models.ChannelTypePrivate || ch.Type == models.ChannelTypeDM {
		if !h.store.IsMember(channelID, requester) {
			c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this channel"})
			return
		}
	}

	if !h.meet.Enabled() {
		// Graceful degrade — 200 so the SPA shows the disabled state rather than
		// surfacing an error.
		c.JSON(http.StatusOK, joinResponse{Enabled: false, Reason: "video not configured"})
		return
	}

	join, err := h.meet.MintJoin(c.Request.Context(), channelID, requester)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not start huddle"})
		return
	}

	talkBase := h.talkBase(c)
	talkToken := h.talkToken(requester, join.ExpiresAt)
	joinURL := buildJoinURL(join.MeetURL, join.Room, join.Token, channelID, talkBase, talkToken)

	c.JSON(http.StatusOK, joinResponse{
		Enabled:     true,
		JoinURL:     joinURL,
		MeetURL:     join.MeetURL,
		Room:        join.Room,
		Token:       join.Token,
		TalkBase:    talkBase,
		TalkChannel: channelID,
		TalkToken:   talkToken,
		ExpiresAt:   join.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// talkBase resolves the public Talk origin Meet's in-call chat calls back to.
// Prefers the configured VULOS_TALK_PUBLIC_URL, else derives it from the
// incoming request (honouring X-Forwarded-Proto behind a proxy).
func (h *HuddleHandler) talkBase(c *gin.Context) string {
	if h.meet.TalkPublicURL != "" {
		return h.meet.TalkPublicURL
	}
	scheme := "http"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// talkToken mints a short-lived Talk session JWT scoped to the requester so
// Meet's in-call chat can post to Talk's message API as that user. Returns "" if
// auth is disabled (the message API then accepts unauthenticated calls) or if no
// signing secret is available.
func (h *HuddleHandler) talkToken(requester string, expiresAt time.Time) string {
	if h.cfg == nil || !h.cfg.Auth.Enabled {
		return ""
	}
	secret, err := middleware.JWTSecret()
	if err != nil {
		return ""
	}
	claims := jwt.RegisteredClaims{
		Subject:   requester,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		return ""
	}
	return signed
}

// buildJoinURL deep-links the Meet web client. The room is carried as ?room=
// (the token already binds video.room, so this is for display/selection) and
// the talk* params make Meet's in-call chat Talk-backed and persistent. See
// vulos-meet/web/src/lib/config.js for the recognised inputs.
func buildJoinURL(meetURL, room, token, channelID, talkBase, talkToken string) string {
	q := url.Values{}
	q.Set("room", room)
	q.Set("token", token)
	q.Set("talkChannel", channelID)
	if talkBase != "" {
		q.Set("talkBase", talkBase)
	}
	if talkToken != "" {
		q.Set("talkToken", talkToken)
	}
	return meetURL + "/?" + q.Encode()
}
