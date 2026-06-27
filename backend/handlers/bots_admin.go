package handlers

import (
	"errors"
	"net/http"
	"time"

	"vulos-talk/backend/middleware"

	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
)

// BotsHandler serves the legacy session/cookie-authed admin API for managing
// bots (/api/bots). It is a COMPAT SHIM over the shared Apps & Bots platform
// registry, kept so the published BOT-API keeps working; the new canonical
// surface is /api/apps. Bots are OWNER-SCOPED: a caller sees and manages only
// apps whose owner_id is their verified account id. An admin (CtxIsAdmin) sees
// and manages all. Only apps targeting Talk are visible here.
type BotsHandler struct {
	reg appsplatform.Registry
}

// NewBotsHandler builds the admin handler over the platform registry.
func NewBotsHandler(reg appsplatform.Registry) *BotsHandler {
	return &BotsHandler{reg: reg}
}

// botSummary is the legacy secret-free BotSummary JSON shape (Talk-historical
// field names) projected from an appsplatform.App.
type botSummary struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	Scopes             []string                    `json:"scopes"`
	EventURL           string                      `json:"event_url"`
	SlashCommands      []appsplatform.SlashCommand `json:"slash_commands"`
	OwnerID            string                      `json:"owner_id"`
	IncomingWebhookID  string                      `json:"incoming_webhook_id"`
	IncomingWebhookURL string                      `json:"incoming_webhook_url"`
	DefaultChannel     string                      `json:"default_channel,omitempty"`
	CreatedAt          time.Time                   `json:"created_at"`
}

// legacyIncomingWebhookPath is the historical incoming-webhook URL Talk exposes.
func legacyIncomingWebhookPath(id string) string { return "/api/bot/hooks/" + id }

func toBotSummary(a *appsplatform.App) botSummary {
	scopes := a.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	cmds := a.SlashCommands
	if cmds == nil {
		cmds = []appsplatform.SlashCommand{}
	}
	return botSummary{
		ID:                 a.ID,
		Name:               a.Name,
		Scopes:             scopes,
		EventURL:           a.WebhookURL,
		SlashCommands:      cmds,
		OwnerID:            a.OwnerID,
		IncomingWebhookID:  a.Incoming.ID,
		IncomingWebhookURL: legacyIncomingWebhookPath(a.Incoming.ID),
		DefaultChannel:     a.DefaultTarget,
		CreatedAt:          a.CreatedAt,
	}
}

// botRequest is the create/update body shape.
type botRequest struct {
	Name           string                      `json:"name"`
	Scopes         []string                    `json:"scopes"`
	EventURL       string                      `json:"event_url"`
	SlashCommands  []appsplatform.SlashCommand `json:"slash_commands"`
	DefaultChannel string                      `json:"default_channel"`
}

// loadOwned fetches an app and enforces owner scoping + Talk targeting. It
// writes a 404 and returns ok=false when the app does not exist, does not target
// Talk, OR the caller is not its owner (and not an admin) — never leaking the
// existence of another owner's app.
func (h *BotsHandler) loadOwned(c *gin.Context) (*appsplatform.App, bool) {
	id := c.Param("id")
	b, err := h.reg.Get(id)
	if err != nil || b == nil || !b.TargetsProduct(appsplatform.ProductTalk) {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return nil, false
	}
	if !c.GetBool(middleware.CtxIsAdmin) && b.OwnerID != requesterID(c) {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return nil, false
	}
	return b, true
}

// badRequestIfValidation maps appsplatform validation errors (unknown scope /
// product) to 400; returns false if it handled the error.
func badRequestIfValidation(c *gin.Context, err error) bool {
	var se *appsplatform.ScopeError
	var pe *appsplatform.ProductError
	if errors.As(err, &se) || errors.As(err, &pe) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return true
	}
	return false
}

// List GET /api/bots → [BotSummary] (no secrets), Talk apps only.
func (h *BotsHandler) List(c *gin.Context) {
	list, err := h.reg.List(requesterID(c), c.GetBool(middleware.CtxIsAdmin))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]botSummary, 0, len(list))
	for _, b := range list {
		if b.TargetsProduct(appsplatform.ProductTalk) {
			out = append(out, toBotSummary(b))
		}
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
	// SSRF guard: reject an event_url pointing at a private/loopback/link-local/
	// metadata target before it is ever stored or dispatched to.
	if err := appsplatform.ValidateWebhookURL(req.EventURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := h.reg.Create(appsplatform.CreateParams{
		Name:            req.Name,
		OwnerID:         requesterID(c),
		Scopes:          req.Scopes,
		Products:        []string{appsplatform.ProductTalk},
		WebhookURL:      req.EventURL,
		SlashCommands:   req.SlashCommands,
		DefaultTarget:   req.DefaultChannel,
		IncomingEnabled: true,
	})
	if err != nil {
		if badRequestIfValidation(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"bot":                  toBotSummary(created.App),
		"token":                created.Token,
		"signing_secret":       created.SigningSecret,
		"incoming_webhook_url": legacyIncomingWebhookPath(created.App.Incoming.ID),
	})
}

// Get GET /api/bots/:id → BotSummary.
func (h *BotsHandler) Get(c *gin.Context) {
	b, ok := h.loadOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toBotSummary(b))
}

// Update PUT /api/bots/:id → BotSummary. Absent JSON fields are left unchanged.
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
	var p appsplatform.UpdateParams
	if _, present := raw["name"]; present {
		if s, ok := raw["name"].(string); ok {
			p.Name = &s
		}
	}
	if _, present := raw["event_url"]; present {
		if s, ok := raw["event_url"].(string); ok {
			// SSRF guard on update too (see Create).
			if err := appsplatform.ValidateWebhookURL(s); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			p.WebhookURL = &s
		}
	}
	if _, present := raw["default_channel"]; present {
		if s, ok := raw["default_channel"].(string); ok {
			p.DefaultTarget = &s
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
		if badRequestIfValidation(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toBotSummary(updated))
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

// Commands GET /api/spaces/commands → [{name, description, app_id}] for the
// composer autocomplete. Session-authed (any member); not owner-scoped because
// any user may invoke any registered slash command.
func (h *BotsHandler) Commands(c *gin.Context) {
	c.JSON(http.StatusOK, h.reg.AllSlashCommands(appsplatform.ProductTalk))
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

func toSlashCommands(v interface{}) []appsplatform.SlashCommand {
	arr, ok := v.([]interface{})
	if !ok {
		return []appsplatform.SlashCommand{}
	}
	out := make([]appsplatform.SlashCommand, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		cmd := appsplatform.SlashCommand{}
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
