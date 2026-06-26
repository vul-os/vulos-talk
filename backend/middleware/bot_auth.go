package middleware

import (
	"errors"
	"net/http"
	"strings"

	"vulos-talk/backend/bots"

	"github.com/gin-gonic/gin"
)

// CtxBot is the gin context key under which BotAuth stores the authenticated
// *bots.Bot for downstream handlers.
const CtxBot = "bot"

// BotAuth authenticates the bot REST API. It expects a Bearer bot token and
// looks the bot up by the token's sha256 HASH in the registry (the plaintext is
// never stored). On any miss it aborts with 401; on success it sets the bot in
// the context (CtxBot).
func BotAuth(reg bots.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bot token required"})
			return
		}
		bot, err := reg.GetByTokenHash(bots.HashToken(token))
		if err != nil || bot == nil {
			if err != nil && !errors.Is(err, bots.ErrNotFound) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "bot registry unavailable"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bot token"})
			return
		}
		c.Set(CtxBot, bot)
		c.Next()
	}
}

// BotFromContext returns the authenticated bot set by BotAuth.
func BotFromContext(c *gin.Context) (*bots.Bot, bool) {
	v, ok := c.Get(CtxBot)
	if !ok {
		return nil, false
	}
	b, ok := v.(*bots.Bot)
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
