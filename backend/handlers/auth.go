// Package handlers — Vulos Talk HTTP handlers.
//
// auth.go is a MINIMAL auth surface for the standalone Talk product. Talk does
// not host its own login UI: the TalkShell's RequireAuth boundary redirects an
// unauthenticated user to the central identity surface (app.vulos.org/login),
// relying on the shared vulos.org session cookie. Talk only needs to *report*
// auth status and verify a presented session for /api/auth/me.
//
// Identity verification reuses office's standalone seam contract: a locally
// signed HS256 JWT validated against the office-managed signing secret
// (middleware.JWTSecret). When auth is disabled (the self-host default) every
// request is the single-user "self" identity.
package handlers

import (
	"net/http"
	"strings"

	"vulos-talk/backend/config"
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// AuthHandler reports auth status for the standalone Talk product.
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler builds the minimal status/me handler.
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

// bearer extracts a bearer token from the Authorization header or the session
// cookie. Returns "" when no credential is presented.
func bearer(c *gin.Context) string {
	token := c.GetHeader("Authorization")
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	} else {
		token = ""
	}
	if token == "" {
		if cookie, err := c.Cookie("session"); err == nil {
			token = cookie
		}
	}
	return token
}

// verify reports whether token is a valid, locally-signed HS256 session.
func verify(token string) bool {
	if token == "" {
		return false
	}
	secret, err := middleware.JWTSecret()
	if err != nil {
		return false
	}
	claims := &jwt.RegisteredClaims{}
	parsed, perr := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return secret, nil
	})
	return perr == nil && parsed.Valid
}

// Status reports whether auth is enforced and whether the caller is signed in.
// GET /api/auth/status
func (h *AuthHandler) Status(c *gin.Context) {
	authenticated := true
	if h.cfg.Auth.Enabled {
		authenticated = verify(bearer(c))
	}
	c.JSON(http.StatusOK, models.AuthStatusResponse{
		Enabled:       h.cfg.Auth.Enabled,
		Authenticated: authenticated,
	})
}

// Me is the auth boundary probe used by every subdomain shell's RequireAuth.
// It returns 401 when auth is enforced and no valid session is presented; the
// shell then redirects to the central login. When auth is disabled it returns
// the single-user "self" identity so the app stays usable for self-host.
// GET /api/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	if !h.cfg.Auth.Enabled {
		c.JSON(http.StatusOK, gin.H{"account_id": "self", "authenticated": false})
		return
	}
	if !verify(bearer(c)) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authenticated": true})
}
