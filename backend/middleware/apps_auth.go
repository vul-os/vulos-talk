package middleware

import (
	"net/http"
	"strings"

	"vulos-talk/backend/config"

	"github.com/golang-jwt/jwt/v5"
)

// AppsAdminAuth returns an appsplatform.AdminAuthFunc-shaped function that
// reuses Talk's OWN session verification for the Apps & Bots management API
// (mounted as a raw net/http handler, so it cannot rely on the gin Auth
// middleware running first).
//
// It mirrors Auth + requesterID: it verifies the session JWT from the
// Authorization header or the `session` cookie and reports (ownerID, isAdmin,
// ok). When auth is disabled it authorizes the local single-user "self"
// identity so self-host installs work without a login.
func AppsAdminAuth(cfg *config.Config) func(r *http.Request) (ownerID string, isAdmin bool, ok bool) {
	return func(r *http.Request) (string, bool, bool) {
		if !cfg.Auth.Enabled {
			return "self", false, true
		}
		token := requestToken(r)
		if token == "" {
			return "", false, false
		}
		secret, err := JWTSecret()
		if err != nil {
			return "", false, false
		}
		claims := &jwt.RegisteredClaims{}
		parsed, perr := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
			// Pin HMAC to reject alg-confusion attacks (matches Auth).
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return secret, nil
		})
		if perr != nil || !parsed.Valid {
			return "", false, false
		}
		isAdmin := false
		for _, aud := range claims.Audience {
			if aud == "vulos:admin" {
				isAdmin = true
				break
			}
		}
		owner := claims.Subject
		if owner == "" {
			owner = "self"
		}
		return owner, isAdmin, true
	}
}

// requestToken extracts the session token from a raw request: the Authorization
// Bearer header or the `session` cookie (matching extractToken in auth.go).
func requestToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie("session"); err == nil {
		return c.Value
	}
	return ""
}
