package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
)

// CtxBot is the gin context key under which BotAuth stores the authenticated
// *appsplatform.App for downstream handlers (the legacy /api/bot/v1 surface).
const CtxBot = "bot"

// BotAuth authenticates the legacy bot REST API against the shared Apps & Bots
// platform registry. It expects a Bearer app token, looks the app up by the
// token's sha256 HASH (the plaintext is never stored), and requires the app to
// target Talk. On any miss it aborts with 401 (403 if the token is valid but the
// app does not target Talk); on success it sets the app in the context (CtxBot).
func BotAuth(reg appsplatform.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bot token required"})
			return
		}
		app, err := reg.GetByTokenHash(appsplatform.HashToken(token))
		if err != nil || app == nil {
			if err != nil && !errors.Is(err, appsplatform.ErrNotFound) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "bot registry unavailable"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bot token"})
			return
		}
		if !app.TargetsProduct(appsplatform.ProductTalk) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "app does not target this product"})
			return
		}
		c.Set(CtxBot, app)
		c.Next()
	}
}

// BotFromContext returns the authenticated app set by BotAuth.
func BotFromContext(c *gin.Context) (*appsplatform.App, bool) {
	v, ok := c.Get(CtxBot)
	if !ok {
		return nil, false
	}
	b, ok := v.(*appsplatform.App)
	return b, ok
}

// bearerToken extracts a Bearer token from the Authorization header only. Unlike
// the session middleware, the bot API does not accept a cookie.
func bearerToken(c *gin.Context) string {
	if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}
