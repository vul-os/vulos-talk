package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vulos-talk/backend/apikey"
	"vulos-talk/backend/config"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// stubIntrospector is a mocked apikey.Introspector for middleware tests.
type stubIntrospector struct {
	res apikey.Result
	err error
}

func (s stubIntrospector) Introspect(_ context.Context, _ string) (apikey.Result, error) {
	return s.res, s.err
}

// talkAuthTestRouter mounts a single protected route guarded by TalkAuth that
// echoes the resolved identity.
func talkAuthTestRouter(cfg *config.Config, intro apikey.Introspector) *gin.Engine {
	r := gin.New()
	g := r.Group("/api/spaces")
	g.Use(TalkAuth(cfg, intro))
	g.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user":   c.GetString(CtxUserID),
			"method": c.GetString(CtxAuthMethod),
			"admin":  c.GetBool(CtxIsAdmin),
		})
	})
	return r
}

func doTalkReq(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/spaces/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTalkAuth_ValidAPIKey(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	intro := stubIntrospector{res: apikey.Result{Valid: true, Account: "alice@vulos.org", Products: []string{"talk"}}}
	r := talkAuthTestRouter(cfg, intro)

	w := doTalkReq(r, "Bearer vk_live_good")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strContains(body, "alice@vulos.org") || !strContains(body, "apikey") {
		t.Fatalf("expected identity from key, got %s", body)
	}
}

func TestTalkAuth_InvalidAPIKey(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	intro := stubIntrospector{res: apikey.Result{Valid: false}}
	r := talkAuthTestRouter(cfg, intro)

	w := doTalkReq(r, "Bearer vk_bad")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestTalkAuth_KeyMissingTalkProduct(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	intro := stubIntrospector{res: apikey.Result{Valid: true, Account: "x", Products: []string{"office"}}}
	r := talkAuthTestRouter(cfg, intro)

	w := doTalkReq(r, "Bearer vk_wrongproduct")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestTalkAuth_IntrospectionUnavailable(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	intro := stubIntrospector{err: errors.New("cp down")}
	r := talkAuthTestRouter(cfg, intro)

	w := doTalkReq(r, "Bearer vk_anything")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestTalkAuth_NoCredsAuthEnabled(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	r := talkAuthTestRouter(cfg, stubIntrospector{})

	w := doTalkReq(r, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestTalkAuth_SelfHostAuthDisabled(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: false}}
	r := talkAuthTestRouter(cfg, nil) // no introspector configured

	// No credentials, auth disabled → allowed as local "self" (not admin).
	w := doTalkReq(r, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 in self-host mode, got %d (%s)", w.Code, w.Body.String())
	}
	if strContains(w.Body.String(), `"admin":true`) {
		t.Fatalf("self-host caller must not be admin: %s", w.Body.String())
	}
}

func TestTalkAuth_KeyIgnoredWhenIntrospectorNil(t *testing.T) {
	// vk_ key presented but introspection NOT configured + auth disabled →
	// falls through to the session path → allowed as self (key not honored).
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: false}}
	r := talkAuthTestRouter(cfg, nil)

	w := doTalkReq(r, "Bearer vk_ignored")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (session fallback), got %d (%s)", w.Code, w.Body.String())
	}
}

// ---- APIKeyAuth (bot surface companion) -------------------------------------

// apiKeyBotRouter simulates the bot API group: APIKeyAuth first, then a stub
// "BotAuth" that would reject any remaining request as having no bot token.
// We use it to prove that a valid vk_ key bypasses the stub BotAuth.
func apiKeyBotRouter(intro apikey.Introspector) *gin.Engine {
	r := gin.New()
	g := r.Group("/api/bot/v1")
	g.Use(APIKeyAuth(intro))
	// Stub replacement for BotAuth: rejects when CtxAuthenticated is not set.
	g.Use(func(c *gin.Context) {
		if !c.GetBool(CtxAuthenticated) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bot token required"})
			return
		}
		c.Next()
	})
	g.GET("/auth.test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user":   c.GetString(CtxUserID),
			"method": c.GetString(CtxAuthMethod),
		})
	})
	return r
}

func doBotReq(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/bot/v1/auth.test", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAPIKeyAuth_ValidKeyOnBotSurface(t *testing.T) {
	intro := stubIntrospector{res: apikey.Result{Valid: true, Account: "dev@vulos.org", Products: []string{"talk"}}}
	r := apiKeyBotRouter(intro)

	w := doBotReq(r, "Bearer vk_live_good")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strContains(body, "dev@vulos.org") || !strContains(body, "apikey") {
		t.Fatalf("expected apikey identity, got %s", body)
	}
}

func TestAPIKeyAuth_NonVKTokenPassesThrough(t *testing.T) {
	intro := stubIntrospector{res: apikey.Result{Valid: true, Account: "x", Products: []string{"talk"}}}
	r := apiKeyBotRouter(intro)

	// A non-vk_ token is not a vk_ key: APIKeyAuth passes through, stub BotAuth rejects.
	w := doBotReq(r, "Bearer vat_bottoken")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from stub BotAuth, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAPIKeyAuth_NilIntrospectorIsNoop(t *testing.T) {
	r := apiKeyBotRouter(nil) // CP not configured

	// Even a vk_ token just passes through to stub BotAuth → 401.
	w := doBotReq(r, "Bearer vk_anything")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (passthrough to stub BotAuth), got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAPIKeyAuth_InvalidKeyOnBotSurface(t *testing.T) {
	intro := stubIntrospector{res: apikey.Result{Valid: false}}
	r := apiKeyBotRouter(intro)

	w := doBotReq(r, "Bearer vk_bad")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
