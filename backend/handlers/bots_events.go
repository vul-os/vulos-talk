package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
)

// BotEventsHandler serves the socket-mode-style SSE event stream at
// GET /api/bot/v1/events (BotAuth). It subscribes the calling app to the shared
// platform dispatcher and streams the SAME event JSON objects (here unsigned over
// the already-authenticated TLS channel) as they occur, cleaning up the
// subscription on disconnect. COMPAT alias of /api/apps/v1/events.
type BotEventsHandler struct {
	disp *appsplatform.Dispatcher
}

// NewBotEventsHandler builds the SSE handler over the dispatcher.
func NewBotEventsHandler(disp *appsplatform.Dispatcher) *BotEventsHandler {
	return &BotEventsHandler{disp: disp}
}

// Stream GET /api/bot/v1/events — text/event-stream of this app's events.
func (h *BotEventsHandler) Stream(c *gin.Context) {
	b, ok := botFrom(c)
	if !ok {
		return
	}

	events, cancel := h.disp.Subscribe(b.ID)
	defer cancel()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	ctx := c.Request.Context()
	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case msg, open := <-events:
			if !open {
				return false
			}
			// SSE frame: "data: <json>\n\n".
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(msg)
			_, _ = w.Write([]byte("\n\n"))
			return true
		}
	})
}
