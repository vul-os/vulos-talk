package bots

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// Token / secret minting.
//
// Security contract:
//   - The bot token is a Bearer secret. Only its sha256 HASH is stored at rest
//     (HashToken); the plaintext is shown ONCE to the operator at create/rotate.
//   - The signing secret is stored as-is because Talk must reproduce it to sign
//     outbound events (see signing.go).
//   - The incoming-webhook id is itself the secret for the unauthenticated
//     incoming-webhook URL, so it is generated with the same CSPRNG.

const (
	tokenPrefix  = "vbt_" // Vulos Bot Token
	secretPrefix = "vbs_" // Vulos Bot Secret (signing)
)

// randHex returns n cryptographically-random bytes hex-encoded (2n chars).
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail; if it does, fail loudly rather than
		// returning a predictable value.
		panic("bots: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// GenerateToken mints a new bot token: "vbt_" + 32 random bytes hex.
func GenerateToken() string { return tokenPrefix + randHex(32) }

// GenerateSecret mints a new signing secret: "vbs_" + 32 random bytes hex.
func GenerateSecret() string { return secretPrefix + randHex(32) }

// GenerateWebhookID mints a random incoming-webhook id (16 random bytes hex).
func GenerateWebhookID() string { return randHex(16) }

// GenerateBotID mints a random bot id (16 random bytes hex).
func GenerateBotID() string { return randHex(16) }

// HashToken returns the sha256 hex of a bot token. This is what is stored and
// what BotAuth looks tokens up by — the plaintext token is never persisted.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
