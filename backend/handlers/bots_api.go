package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
)

// BotAPIHandler serves the legacy Bearer-app-token API at /api/bot/v1 plus the
// unauthenticated incoming-webhook endpoint. It is a COMPAT SHIM kept so the
// published BOT-API and the echo-bot example keep working after the migration to
// the shared Apps & Bots platform: it is backed by the SAME appsplatform
// registry as the new /api/apps surface, so a bot created via either place is
// one app. Apps post/act as the synthetic account "app:<id>".
type BotAPIHandler struct {
	spaces *SpacesHandlerExt
	reg    appsplatform.Registry
	sink   BotSink // dispatcher; may be nil
}

// NewBotAPIHandler wires the bot API over the shared spaces handler + registry.
// sink (the dispatcher) is optional; when set, bot-posted messages emit events.
func NewBotAPIHandler(spaces *SpacesHandlerExt, reg appsplatform.Registry, sink BotSink) *BotAPIHandler {
	return &BotAPIHandler{spaces: spaces, reg: reg, sink: sink}
}

// requireScope enforces a scope, writing 403 and returning false on miss.
func requireScope(c *gin.Context, b *appsplatform.App, scope string) bool {
	if !b.HasScope(scope) {
		c.JSON(http.StatusForbidden, gin.H{"error": "missing required scope: " + scope})
		return false
	}
	return true
}

// botFrom returns the authenticated app or writes 401.
func botFrom(c *gin.Context) (*appsplatform.App, bool) {
	b, ok := middleware.BotFromContext(c)
	if !ok || b == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bot not authenticated"})
		return nil, false
	}
	return b, true
}

// requireBotChannel enforces an app's access to a channel (public OK; private/DM
// require the app to be a member). Writes 404/403 and returns false on denial.
func (h *BotAPIHandler) requireBotChannel(c *gin.Context, b *appsplatform.App, channelID string) bool {
	allowed, exists := h.spaces.canAccessChannel(channelID, b.AccountID())
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "bot is not a member of this channel"})
		return false
	}
	return true
}

// AuthTest GET /api/bot/v1/auth.test → {bot_id, name, scopes}. No scope needed.
func (h *BotAPIHandler) AuthTest(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	scopes := b.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"bot_id": b.ID, "name": b.Name, "scopes": scopes})
}

// PostMessage POST /api/bot/v1/messages. Scope chat:write.
func (h *BotAPIHandler) PostMessage(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	if !requireScope(c, b, appsplatform.ScopeChatWrite) {
		return
	}
	var req struct {
		ChannelID    string `json:"channel_id"`
		Text         string `json:"text"`
		ThreadParent string `json:"thread_parent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.ChannelID) == "" || strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id and text required"})
		return
	}
	if !h.requireBotChannel(c, b, req.ChannelID) {
		return
	}
	msg, err := h.spaces.store.SendMessage(req.ChannelID, b.AccountID(), req.Text, req.ThreadParent)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.sink != nil {
		h.sink.OnMessageCreated(msg.ChannelID, msg.ID, msg.AuthorID, msg.Body, msg.ThreadParent)
	}
	c.JSON(http.StatusCreated, msg)
}

// ListChannels GET /api/bot/v1/channels. Scope channels:read.
// Returns public channels plus private/DM channels the bot is a member of.
func (h *BotAPIHandler) ListChannels(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	if !requireScope(c, b, appsplatform.ScopeChannelsRead) {
		return
	}
	out := make([]*models.Channel, 0)
	for _, ch := range h.spaces.store.ListChannels() {
		switch ch.Type {
		case models.ChannelTypePrivate, models.ChannelTypeDM:
			if h.spaces.store.IsMember(ch.ID, b.AccountID()) {
				out = append(out, ch)
			}
		default:
			out = append(out, ch)
		}
	}
	c.JSON(http.StatusOK, out)
}

// History GET /api/bot/v1/channels/:channelId/history?limit=N. Scope history:read.
func (h *BotAPIHandler) History(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	if !requireScope(c, b, appsplatform.ScopeHistoryRead) {
		return
	}
	channelID := c.Param("channelId")
	if !h.requireBotChannel(c, b, channelID) {
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	msgs := h.spaces.store.ListMessages(channelID) // ascending by SeqClock
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:] // most-recent N, kept in chronological order
	}
	if msgs == nil {
		msgs = []*models.Message{}
	}
	c.JSON(http.StatusOK, msgs)
}

// Members GET /api/bot/v1/channels/:channelId/members. Scope members:read.
func (h *BotAPIHandler) Members(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	if !requireScope(c, b, appsplatform.ScopeMembersRead) {
		return
	}
	channelID := c.Param("channelId")
	if !h.requireBotChannel(c, b, channelID) {
		return
	}
	members := h.spaces.store.ListMembers(channelID)
	if members == nil {
		members = []*models.Membership{}
	}
	c.JSON(http.StatusOK, members)
}

// reactionBody is the shared body for add/remove reaction.
type reactionBody struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

// AddReaction POST /api/bot/v1/reactions. Scope reactions:write.
func (h *BotAPIHandler) AddReaction(c *gin.Context) {
	h.reaction(c, true)
}

// RemoveReaction DELETE /api/bot/v1/reactions. Scope reactions:write.
func (h *BotAPIHandler) RemoveReaction(c *gin.Context) {
	h.reaction(c, false)
}

func (h *BotAPIHandler) reaction(c *gin.Context, add bool) {
	b, ok := botFrom(c)
	if !ok {
		return
	}
	if !requireScope(c, b, appsplatform.ScopeReactionsWrite) {
		return
	}
	var req reactionBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Emoji) == "" || strings.TrimSpace(req.MessageID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message_id and emoji required"})
		return
	}
	if !h.requireBotChannel(c, b, req.ChannelID) {
		return
	}
	if _, found := h.spaces.store.GetMessage(req.ChannelID, req.MessageID); !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found in channel"})
		return
	}
	if add {
		h.spaces.ext.reactions.Add(req.MessageID, req.Emoji, b.AccountID())
	} else {
		h.spaces.ext.reactions.Remove(req.MessageID, req.Emoji, b.AccountID())
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// IncomingWebhook POST /api/bot/hooks/:webhookId — unauthenticated (the id is
// the secret). Posts text to channel_id (or the app's default target, or
// "general") as the app. 404 when the webhook id is unknown or disabled.
func (h *BotAPIHandler) IncomingWebhook(c *gin.Context) {
	webhookID := c.Param("webhookId")
	b, err := h.reg.GetByIncomingWebhookID(webhookID)
	if err != nil || b == nil || !b.TargetsProduct(appsplatform.ProductTalk) || !b.Incoming.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown webhook"})
		return
	}
	var req struct {
		Text      string `json:"text"`
		ChannelID string `json:"channel_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}
	channelID := strings.TrimSpace(req.ChannelID)
	if channelID == "" {
		channelID = b.DefaultTarget
	}
	if channelID == "" {
		channelID = "general"
	}
	// SECURITY: gate the webhook through the SAME channel authz the authenticated
	// REST path uses. A webhook-id holder must not be able to post into a
	// private/DM channel the app is not a member of just because the channel
	// exists. Public channels remain open; private/DM require app membership.
	if !h.requireBotChannel(c, b, channelID) {
		return
	}
	msg, err := h.spaces.store.SendMessage(channelID, b.AccountID(), req.Text, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.sink != nil {
		h.sink.OnMessageCreated(msg.ChannelID, msg.ID, msg.AuthorID, msg.Body, msg.ThreadParent)
	}
	c.JSON(http.StatusCreated, msg)
}
