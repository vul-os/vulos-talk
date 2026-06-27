// Command echo-bot is a tiny, dependency-free example of a Vulos Talk bot.
//
// It runs an HTTP server that receives signed outbound events from Talk,
// verifies the X-Vulos-Signature over "<timestamp>.<rawBody>" using the bot's
// signing secret, and — on app_mention or slash_command events — calls back the
// Talk bot REST API (POST /api/bot/v1/messages) with the bot token to echo the
// text into the originating channel.
//
// It uses only the Go standard library (crypto/hmac, crypto/sha256,
// encoding/hex, net/http) so it compiles as part of `go build ./...` with no
// external module dependencies.
//
// Usage:
//
//	VULOS_TALK_BASE_URL=http://localhost:8080 \
//	VULOS_BOT_TOKEN=vbt_... \
//	VULOS_BOT_SIGNING_SECRET=vbs_... \
//	go run ./examples/echo-bot
//
// Then point your bot's event_url at http://<this-host>:8090/events.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// event mirrors the Talk outbound event envelope (Vulos Apps & Bots platform).
type event struct {
	Type      string                 `json:"type"`
	AppID     string                 `json:"app_id"`
	Product   string                 `json:"product"`
	Event     map[string]interface{} `json:"event"`
	EventTime int64                  `json:"event_time"`
}

const (
	sigHeaderTimestamp = "X-Vulos-Request-Timestamp"
	sigHeaderSignature = "X-Vulos-Signature"
	maxSkewSeconds     = 300 // reject events older than 5 minutes (replay defense)
)

func main() {
	base := env("VULOS_TALK_BASE_URL", "http://localhost:8080")
	token := os.Getenv("VULOS_BOT_TOKEN")
	secret := os.Getenv("VULOS_BOT_SIGNING_SECRET")
	addr := env("ECHO_BOT_ADDR", ":8090")
	if token == "" || secret == "" {
		log.Fatal("set VULOS_BOT_TOKEN and VULOS_BOT_SIGNING_SECRET")
	}

	b := &echoBot{base: base, token: token, secret: secret, client: &http.Client{Timeout: 5 * time.Second}}
	http.HandleFunc("/events", b.handleEvents)
	log.Printf("echo-bot listening on %s, talking to %s", addr, base)
	log.Fatal(http.ListenAndServe(addr, nil))
}

type echoBot struct {
	base   string
	token  string
	secret string
	client *http.Client
}

func (b *echoBot) handleEvents(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	ts := r.Header.Get(sigHeaderTimestamp)
	sig := r.Header.Get(sigHeaderSignature)
	if !verify(ts, body, b.secret, sig) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	// Always 200 quickly; do the echo work without blocking the ack.
	w.WriteHeader(http.StatusOK)

	var ev event
	if err := json.Unmarshal(body, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "app_mention", "slash_command":
		channelID, _ := ev.Event["channel_id"].(string)
		text, _ := ev.Event["text"].(string)
		if channelID == "" {
			return
		}
		go b.postMessage(channelID, "echo: "+text)
	}
}

func (b *echoBot) postMessage(channelID, text string) {
	payload, _ := json.Marshal(map[string]string{"channel_id": channelID, "text": text})
	req, err := http.NewRequest(http.MethodPost, b.base+"/api/bot/v1/messages", bytes.NewReader(payload))
	if err != nil {
		log.Printf("build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		log.Printf("post message: %v", err)
		return
	}
	resp.Body.Close()
}

// verify reproduces the v0 HMAC-SHA256 signature over "<timestamp>.<body>" and
// compares it in constant time, rejecting stale timestamps.
func verify(timestamp string, body []byte, secret, sig string) bool {
	if timestamp == "" || sig == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if diff := time.Now().Unix() - ts; diff > maxSkewSeconds || diff < -maxSkewSeconds {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
