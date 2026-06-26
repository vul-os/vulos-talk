package handlers

import (
	"net/http"
	"testing"

	"vulos-talk/backend/bots"
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
)

// adminRouter wires the bots admin API behind a fake session-auth middleware
// that injects the given verified user / admin flag.
func adminRouter(reg bots.Registry, user string, admin bool) *gin.Engine {
	h := NewBotsHandler(reg)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, user)
		if admin {
			c.Set(middleware.CtxIsAdmin, true)
		}
		c.Next()
	})
	r.GET("/api/bots", h.List)
	r.POST("/api/bots", h.Create)
	r.GET("/api/bots/:id", h.Get)
	r.PUT("/api/bots/:id", h.Update)
	r.DELETE("/api/bots/:id", h.Delete)
	r.POST("/api/bots/:id/rotate-token", h.RotateToken)
	return r
}

func TestAdminOwnerScoping(t *testing.T) {
	reg := bots.NewMemoryRegistry()

	// alice creates a bot through the API.
	ra := adminRouter(reg, "alice", false)
	w := doReq(ra, http.MethodPost, "/api/bots", map[string]interface{}{
		"name":   "alicebot",
		"scopes": []string{"chat:write"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created struct {
		Bot   bots.Summary `json:"bot"`
		Token string       `json:"token"`
	}
	mustDecode(t, w, &created)
	if created.Token == "" || created.Bot.ID == "" {
		t.Fatalf("expected token + bot id, got %+v", created)
	}
	id := created.Bot.ID

	// bob cannot see or modify alice's bot.
	rb := adminRouter(reg, "bob", false)
	if w := doReq(rb, http.MethodGet, "/api/bots/"+id, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob GET alice's bot: expected 404, got %d", w.Code)
	}
	if w := doReq(rb, http.MethodPut, "/api/bots/"+id, map[string]interface{}{"name": "hijack"}); w.Code != http.StatusNotFound {
		t.Fatalf("bob PUT alice's bot: expected 404, got %d", w.Code)
	}
	if w := doReq(rb, http.MethodDelete, "/api/bots/"+id, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob DELETE alice's bot: expected 404, got %d", w.Code)
	}
	if w := doReq(rb, http.MethodPost, "/api/bots/"+id+"/rotate-token", nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob rotate alice's bot: expected 404, got %d", w.Code)
	}

	// bob's list does not include alice's bot.
	w = doReq(rb, http.MethodGet, "/api/bots", nil)
	var bobList []bots.Summary
	mustDecode(t, w, &bobList)
	if len(bobList) != 0 {
		t.Fatalf("bob should see 0 bots, got %d", len(bobList))
	}

	// alice can manage her own bot.
	if w := doReq(ra, http.MethodGet, "/api/bots/"+id, nil); w.Code != http.StatusOK {
		t.Fatalf("alice GET own bot: expected 200, got %d", w.Code)
	}

	// an admin sees everyone's bots.
	radmin := adminRouter(reg, "carol", true)
	w = doReq(radmin, http.MethodGet, "/api/bots", nil)
	var adminList []bots.Summary
	mustDecode(t, w, &adminList)
	if len(adminList) != 1 {
		t.Fatalf("admin should see all bots, got %d", len(adminList))
	}
	// and can read alice's bot directly.
	if w := doReq(radmin, http.MethodGet, "/api/bots/"+id, nil); w.Code != http.StatusOK {
		t.Fatalf("admin GET alice's bot: expected 200, got %d", w.Code)
	}
}

func TestCreateBotResponseHasNoStoredSecrets(t *testing.T) {
	reg := bots.NewMemoryRegistry()
	ra := adminRouter(reg, "alice", false)
	w := doReq(ra, http.MethodPost, "/api/bots", map[string]interface{}{"name": "b"})
	var created struct {
		Bot                bots.Summary `json:"bot"`
		Token              string       `json:"token"`
		SigningSecret      string       `json:"signing_secret"`
		IncomingWebhookURL string       `json:"incoming_webhook_url"`
	}
	mustDecode(t, w, &created)
	if created.SigningSecret == "" || created.IncomingWebhookURL == "" {
		t.Fatalf("expected signing_secret + incoming_webhook_url, got %+v", created)
	}
	// Stored token is only a hash, never the plaintext.
	stored, _ := reg.Get(created.Bot.ID)
	if stored.TokenHash == created.Token || stored.TokenHash != bots.HashToken(created.Token) {
		t.Fatalf("token stored incorrectly (must be sha256 hash)")
	}
}

// TestSlashDispatchInterceptsRegisteredCommand proves the send path intercepts a
// registered slash command and stores nothing, while an unknown command posts
// normally.
func TestSlashDispatchInterceptsRegisteredCommand(t *testing.T) {
	sp := testHandler(t)
	reg := bots.NewMemoryRegistry()
	_, _ = reg.Create(bots.CreateParams{Name: "ci", OwnerID: "alice", SlashCommands: []bots.SlashCommand{{Name: "deploy"}}})
	disp := bots.NewDispatcher(reg, sp.store)
	sp.SetBotSink(disp)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, "alice")
		c.Next()
	})
	r.POST("/spaces/channels/:channelId/messages", sp.SendMessage)

	// Registered command → 200 {slash:true}, NOT stored.
	w := doReq(r, http.MethodPost, "/spaces/channels/general/messages", models.SendMessageRequest{Body: "/deploy prod"})
	if w.Code != http.StatusOK {
		t.Fatalf("slash command: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var slashResp struct {
		Slash      bool   `json:"slash"`
		Command    string `json:"command"`
		Dispatched bool   `json:"dispatched"`
	}
	mustDecode(t, w, &slashResp)
	if !slashResp.Slash || slashResp.Command != "deploy" || !slashResp.Dispatched {
		t.Fatalf("unexpected slash response: %+v", slashResp)
	}
	for _, m := range sp.store.ListMessages("general") {
		if m.Body == "/deploy prod" {
			t.Fatalf("slash command was stored as a message")
		}
	}

	// Unknown command → stored as a normal message (201).
	w = doReq(r, http.MethodPost, "/spaces/channels/general/messages", models.SendMessageRequest{Body: "/unknown thing"})
	if w.Code != http.StatusCreated {
		t.Fatalf("unknown command: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
}
