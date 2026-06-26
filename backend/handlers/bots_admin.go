package handlers

import (
	"errors"
	"net/http"

	"vulos-talk/backend/bots"
	"vulos-talk/backend/middleware"

	"github.com/gin-gonic/gin"
)

// BotsHandler serves the session/cookie-authed admin API for managing bots.
// Bots are OWNER-SCOPED: a caller sees and manages only bots whose owner_id is
// their verified account id. An admin (CtxIsAdmin) sees and manages all bots.
type BotsHandler struct {
	reg bots.Registry
}

// NewBotsHandler builds the admin handler over a registry.
func NewBotsHandler(reg bots.Registry) *BotsHandler {
	return &BotsHandler{reg: reg}
}

// botRequest is the create/update body shape (PUT treats absent fields as
// "leave unchanged" via pointers built in Update).
type botRequest struct {
	Name           string              `json:"name"`
	Scopes         []string            `json:"scopes"`
	EventURL       string              `json:"event_url"`
	SlashCommands  []bots.SlashCommand `json:"slash_commands"`
	DefaultChannel string              `json:"default_channel"`
}

// loadOwned fetches a bot and enforces owner scoping. It writes a 404 and
// returns ok=false when the bot does not exist OR the caller is not its owner
// (and not an admin) — never leaking the existence of another owner's bot.
func (h *BotsHandler) loadOwned(c *gin.Context) (*bots.Bot, bool) {
	id := c.Param("id")
	b, err := h.reg.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return nil, false
	}
	if !c.GetBool(middleware.CtxIsAdmin) && b.OwnerID != requesterID(c) {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return nil, false
	}
	return b, true
}

// List GET /api/bots → [Summary] (no secrets).
func (h *BotsHandler) List(c *gin.Context) {
	list, err := h.reg.List(requesterID(c), c.GetBool(middleware.CtxIsAdmin))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]bots.Summary, 0, len(list))
	for _, b := range list {
		out = append(out, b.ToSummary())
	}
	c.JSON(http.StatusOK, out)
}

// Create POST /api/bots → {bot, token, signing_secret, incoming_webhook_url}.
func (h *BotsHandler) Create(c *gin.Context) {
	var req botRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	created, err := h.reg.Create(bots.CreateParams{
		Name:           req.Name,
		OwnerID:        requesterID(c),
		Scopes:         req.Scopes,
		EventURL:       req.EventURL,
		SlashCommands:  req.SlashCommands,
		DefaultChannel: req.DefaultChannel,
	})
	if err != nil {
		var se *bots.ScopeError
		if errors.As(err, &se) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bot":                  created.Bot.ToSummary(),
		"token":                created.Token,
		"signing_secret":       created.SigningSecret,
		"incoming_webhook_url": bots.IncomingWebhookPath(created.Bot.IncomingWebhookID),
	})
}

// Get GET /api/bots/:id → Summary.
func (h *BotsHandler) Get(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, b.ToSummary())
}

// Update PUT /api/bots/:id → Summary.
func (h *BotsHandler) Update(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	// Bind into a map first so we can distinguish absent fields from zero values.
	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var p bots.UpdateParams
	if _, present := raw["name"]; present {
		if s, ok := raw["name"].(string); ok {
			p.Name = &s
		}
	}
	if _, present := raw["event_url"]; present {
		if s, ok := raw["event_url"].(string); ok {
			p.EventURL = &s
		}
	}
	if _, present := raw["default_channel"]; present {
		if s, ok := raw["default_channel"].(string); ok {
			p.DefaultChannel = &s
		}
	}
	if v, present := raw["scopes"]; present {
		scopes := toStringSlice(v)
		p.Scopes = &scopes
	}
	if v, present := raw["slash_commands"]; present {
		cmds := toSlashCommands(v)
		p.SlashCommands = &cmds
	}
	updated, err := h.reg.Update(b.ID, p)
	if err != nil {
		var se *bots.ScopeError
		if errors.As(err, &se) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated.ToSummary())
}

// Delete DELETE /api/bots/:id → {ok:true}.
func (h *BotsHandler) Delete(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	if err := h.reg.Delete(b.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RotateToken POST /api/bots/:id/rotate-token → {token}.
func (h *BotsHandler) RotateToken(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	token, err := h.reg.RotateToken(b.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// RotateSecret POST /api/bots/:id/rotate-secret → {signing_secret}.
func (h *BotsHandler) RotateSecret(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	secret, err := h.reg.RotateSecret(b.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"signing_secret": secret})
}

// Commands GET /api/spaces/commands → [{name, description, bot_id}] for the
// composer autocomplete. Session-authed (any member); not owner-scoped because
// any user may invoke any registered slash command.
func (h *BotsHandler) Commands(c *gin.Context) {
	c.JSON(http.StatusOK, h.reg.AllSlashCommands())
}

// ---- JSON coercion helpers (for the partial-update map decode) ---------------

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toSlashCommands(v interface{}) []bots.SlashCommand {
	arr, ok := v.([]interface{})
	if !ok {
		return []bots.SlashCommand{}
	}
	out := make([]bots.SlashCommand, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		cmd := bots.SlashCommand{}
		if s, ok := m["name"].(string); ok {
			cmd.Name = s
		}
		if s, ok := m["description"].(string); ok {
			cmd.Description = s
		}
		out = append(out, cmd)
	}
	return out
}
