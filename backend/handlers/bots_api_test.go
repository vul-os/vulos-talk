package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
)

// talkApp creates a Talk-targeting app with an enabled incoming webhook, as the
// migrated compat surface requires.
func talkApp(t *testing.T, reg appsplatform.Registry, name string, scopes ...string) *appsplatform.Created {
	t.Helper()
	created, err := reg.Create(appsplatform.CreateParams{
		Name:            name,
		OwnerID:         "alice",
		Scopes:          scopes,
		Products:        []string{appsplatform.ProductTalk},
		IncomingEnabled: true,
	})
	if err != nil {
		t.Fatalf("create app %q: %v", name, err)
	}
	return created
}

// botAPIRouter wires the bot REST API behind BotAuth over a fresh in-memory
// platform registry + spaces handler. Returns the router, registry, and spaces
// handler.
func botAPIRouter(t *testing.T) (*gin.Engine, appsplatform.Registry, *SpacesHandlerExt) {
	t.Helper()
	sp := testHandler(t)
	reg := appsplatform.NewMemoryRegistry()
	disp := appsplatform.NewDispatcher(reg, appsplatform.ProductTalk)
	adapter := NewTalkAdapter(sp)
	sink := NewAppsSink(reg, disp, adapter)
	sp.SetBotSink(sink)
	api := NewBotAPIHandler(sp, reg, sink)

	r := gin.New()
	g := r.Group("/api/bot/v1")
	g.Use(middleware.BotAuth(reg))
	g.GET("/auth.test", api.AuthTest)
	g.POST("/messages", api.PostMessage)
	g.GET("/channels", api.ListChannels)
	g.GET("/channels/:channelId/history", api.History)
	g.GET("/channels/:channelId/members", api.Members)
	g.POST("/reactions", api.AddReaction)
	g.DELETE("/reactions", api.RemoveReaction)
	r.POST("/api/bot/hooks/:webhookId", api.IncomingWebhook)
	return r, reg, sp
}

func botReq(r *gin.Engine, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBotAuth_HashLookupAndReject(t *testing.T) {
	r, reg, _ := botAPIRouter(t)
	created := talkApp(t, reg, "b")

	// Valid token → 200 (auth.test needs no scope).
	if w := botReq(r, http.MethodGet, "/api/bot/v1/auth.test", created.Token, nil); w.Code != http.StatusOK {
		t.Fatalf("valid token: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	// No token → 401.
	if w := botReq(r, http.MethodGet, "/api/bot/v1/auth.test", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", w.Code)
	}
	// Bad token → 401.
	if w := botReq(r, http.MethodGet, "/api/bot/v1/auth.test", "vat_bogus", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: expected 401, got %d", w.Code)
	}
}

func TestBotScopeEnforcement_ChatWrite(t *testing.T) {
	r, reg, _ := botAPIRouter(t)

	// App WITHOUT chat:write is forbidden from posting.
	noScope := talkApp(t, reg, "ro")
	w := botReq(r, http.MethodPost, "/api/bot/v1/messages", noScope.Token,
		map[string]string{"channel_id": "general", "text": "hi"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("no chat:write: expected 403, got %d (%s)", w.Code, w.Body.String())
	}

	// App WITH chat:write may post to a public channel; author is app:<id>.
	rw := talkApp(t, reg, "rw", appsplatform.ScopeChatWrite)
	w = botReq(r, http.MethodPost, "/api/bot/v1/messages", rw.Token,
		map[string]string{"channel_id": "general", "text": "hi"})
	if w.Code != http.StatusCreated {
		t.Fatalf("with chat:write: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	var msg models.Message
	mustDecode(t, w, &msg)
	if msg.AuthorID != appsplatform.AppAccountID(rw.App.ID) {
		t.Fatalf("expected author %q, got %q", appsplatform.AppAccountID(rw.App.ID), msg.AuthorID)
	}
}

func TestBotMessagePostScoping_PrivateChannel(t *testing.T) {
	r, reg, sp := botAPIRouter(t)
	bot := talkApp(t, reg, "b", appsplatform.ScopeChatWrite, appsplatform.ScopeHistoryRead)

	// Private channel the app is NOT a member of.
	ch, _ := sp.store.CreateChannel("secret", models.ChannelTypePrivate, "alice")
	_, _ = sp.store.AddMember(ch.ID, "alice")

	// Posting → 403.
	w := botReq(r, http.MethodPost, "/api/bot/v1/messages", bot.Token,
		map[string]string{"channel_id": ch.ID, "text": "intrusion"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-member post: expected 403, got %d (%s)", w.Code, w.Body.String())
	}
	// Reading history → 403.
	w = botReq(r, http.MethodGet, "/api/bot/v1/channels/"+ch.ID+"/history", bot.Token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-member history: expected 403, got %d", w.Code)
	}

	// Once the app is added as a member, both succeed.
	_, _ = sp.store.AddMember(ch.ID, appsplatform.AppAccountID(bot.App.ID))
	w = botReq(r, http.MethodPost, "/api/bot/v1/messages", bot.Token,
		map[string]string{"channel_id": ch.ID, "text": "now allowed"})
	if w.Code != http.StatusCreated {
		t.Fatalf("member post: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	w = botReq(r, http.MethodGet, "/api/bot/v1/channels/"+ch.ID+"/history", bot.Token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("member history: expected 200, got %d", w.Code)
	}
}

func TestBotHistoryScopeMissing(t *testing.T) {
	r, reg, _ := botAPIRouter(t)
	bot := talkApp(t, reg, "b") // no history:read
	w := botReq(r, http.MethodGet, "/api/bot/v1/channels/general/history", bot.Token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing history:read: expected 403, got %d", w.Code)
	}
}

func TestIncomingWebhook(t *testing.T) {
	r, reg, _ := botAPIRouter(t)
	created := talkApp(t, reg, "hooky")

	// Unknown webhook id → 404.
	if w := botReq(r, http.MethodPost, "/api/bot/hooks/nope", "", map[string]string{"text": "x"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown webhook: expected 404, got %d", w.Code)
	}

	// Known id posts to general as the app.
	w := botReq(r, http.MethodPost, "/api/bot/hooks/"+created.App.Incoming.ID, "",
		map[string]string{"text": "deploy finished"})
	if w.Code != http.StatusCreated {
		t.Fatalf("incoming webhook: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	var msg models.Message
	mustDecode(t, w, &msg)
	if msg.AuthorID != appsplatform.AppAccountID(created.App.ID) || msg.ChannelID != "general" {
		t.Fatalf("unexpected webhook message: %+v", msg)
	}
}

func TestBotReactionScope(t *testing.T) {
	r, reg, sp := botAPIRouter(t)
	bot := talkApp(t, reg, "b", appsplatform.ScopeReactionsWrite)
	msg, _ := sp.store.SendMessage("general", "alice", "react to me", "")

	w := botReq(r, http.MethodPost, "/api/bot/v1/reactions", bot.Token,
		map[string]string{"channel_id": "general", "message_id": msg.ID, "emoji": "👍"})
	if w.Code != http.StatusOK {
		t.Fatalf("add reaction: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	// An app lacking reactions:write is denied.
	noScope := talkApp(t, reg, "n")
	w = botReq(r, http.MethodPost, "/api/bot/v1/reactions", noScope.Token,
		map[string]string{"channel_id": "general", "message_id": msg.ID, "emoji": "👍"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("no reactions:write: expected 403, got %d", w.Code)
	}
}
