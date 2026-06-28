package middleware

import (
	"net/http"
	"strings"

	"vulos-talk/backend/apikey"
	"vulos-talk/backend/config"

	"github.com/gin-gonic/gin"
)

// Context keys set by TalkAuth in addition to the shared identity keys
// (CtxUserID, CtxIsAdmin, CtxAuthenticated) so handlers can introspect HOW
// the caller authenticated and WHICH scopes an API key carries.
const (
	// CtxAuthMethod is "session" or "apikey".
	CtxAuthMethod = "authMethod"
	// CtxScopes holds the []string scopes a vk_ key carries (empty for sessions).
	CtxScopes = "scopes"
)

// TalkAuth authenticates requests to the Talk /api/spaces/* and bot REST API
// surfaces, accepting EITHER:
//
//   - a Vulos API key — `Authorization: Bearer vk_…` — validated via the cloud
//     introspection seam (apikey.Introspector). The key must be valid and carry
//     the "talk" product scope, OR
//   - the existing Talk session — `Authorization: Bearer <jwt>` or the HttpOnly
//     "session" cookie, HS256-verified exactly like middleware.Auth.
//
// A vk_ key is only attempted when an introspector is wired (intro != nil, i.e.
// VULOS_CP_BASE_URL is configured). When it is NOT configured the key path is
// disabled and only session auth applies — self-host is unchanged.
//
// Unlike the SPA root gate, the API surfaces NEVER redirect: every failure is
// a JSON error body with the appropriate status (401/403/503).
//
// On success it sets CtxAuthenticated + CtxUserID (and CtxIsAdmin for an admin
// session) so the existing handler identity logic works unchanged.
func TalkAuth(cfg *config.Config, intro apikey.Introspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := apikeyBearerRaw(c)

		// ── API-key path ──────────────────────────────────────────────────────
		// Only when an introspector is configured AND the credential looks like a
		// Vulos API key. A vk_ token is never tried as a session JWT (and vice
		// versa), so the two schemes can't be confused.
		if intro != nil && strings.HasPrefix(raw, apikey.KeyPrefix) {
			res, err := intro.Introspect(c.Request.Context(), raw)
			if err != nil {
				// CP unreachable: fail closed rather than guess.
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "API key validation unavailable"})
				return
			}
			if !res.Valid {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
				return
			}
			if !res.HasProduct(apikey.ProductTalk) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API key not authorized for the talk product"})
				return
			}
			c.Set(CtxAuthenticated, true)
			c.Set(CtxUserID, res.Account)
			c.Set(CtxScopes, res.Scopes)
			c.Set(CtxAuthMethod, "apikey")
			// API keys never carry the admin scope: a key acts only as its own
			// account, never as a tenant-wide admin.
			c.Next()
			return
		}

		// ── Session path ──────────────────────────────────────────────────────
		// Self-host single-user (auth disabled): allow; handlers fall back to
		// the local "self" identity and the caller is NOT an admin (parity with
		// the existing /api protected group).
		if !cfg.Auth.Enabled {
			c.Set(CtxAuthMethod, "session")
			c.Next()
			return
		}

		// Multi-tenant: verify the session token (Authorization bearer or the
		// HttpOnly "session" cookie) using HS256 — exactly like Auth().
		token := extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		subject, isAdmin, ok := SessionIdentity(cfg, token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
			return
		}
		c.Set(CtxAuthenticated, true)
		c.Set(CtxUserID, subject)
		if isAdmin {
			c.Set(CtxIsAdmin, true)
		}
		c.Set(CtxAuthMethod, "session")
		c.Next()
	}
}

// APIKeyAuth is a narrower companion to TalkAuth designed for routes that
// already have their OWN non-session auth (e.g. the bot REST API which uses
// Bearer app tokens). It intercepts ONLY `Authorization: Bearer vk_…`
// requests, validates them via the introspection seam, and — on success —
// sets the same context keys as TalkAuth so downstream handlers are identity-
// agnostic. For any other credential shape it calls c.Next() immediately so
// the following handler (e.g. BotAuth) takes over.
//
// This lets a developer use EITHER a bot-app token OR a vk_ API key on the
// /api/bot/v1 surface without changing BotAuth.
//
// When intro is nil (VULOS_CP_BASE_URL not set) the middleware is a no-op
// passthrough so the existing bot-token auth is unchanged on self-host.
func APIKeyAuth(intro apikey.Introspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		if intro == nil {
			c.Next()
			return
		}
		raw := apikeyBearerRaw(c)
		if !strings.HasPrefix(raw, apikey.KeyPrefix) {
			// Not a vk_ credential — let the next handler (BotAuth) verify it.
			c.Next()
			return
		}
		res, err := intro.Introspect(c.Request.Context(), raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "API key validation unavailable"})
			return
		}
		if !res.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
			return
		}
		if !res.HasProduct(apikey.ProductTalk) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API key not authorized for the talk product"})
			return
		}
		c.Set(CtxAuthenticated, true)
		c.Set(CtxUserID, res.Account)
		c.Set(CtxScopes, res.Scopes)
		c.Set(CtxAuthMethod, "apikey")
		c.Next()
	}
}

// apikeyBearerRaw returns the raw token from an `Authorization: Bearer <token>`
// header (no scheme, trimmed), or "" when absent.
func apikeyBearerRaw(c *gin.Context) string {
	if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}
