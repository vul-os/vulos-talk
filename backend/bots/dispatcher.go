package bots

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"vulos-talk/backend/models"
)

// Event types delivered to a bot's event_url and SSE stream.
const (
	EventMessageCreated = "message.created"
	EventAppMention     = "app_mention"
	EventMemberJoined   = "member_joined"
	EventSlashCommand   = "slash_command"
)

// Event is the outbound event envelope. The Event field carries the per-type
// payload (a map so each type can carry its own shape).
type Event struct {
	Type      string                 `json:"type"`
	BotID     string                 `json:"bot_id"`
	Team      string                 `json:"team"`
	Event     map[string]interface{} `json:"event"`
	EventTime int64                  `json:"event_time"`
}

// Spaces is the minimal view of the message store the dispatcher needs to decide
// channel visibility for event fan-out. *spaces.SpacesStore satisfies it.
type Spaces interface {
	GetChannel(id string) (*models.Channel, bool)
	IsMember(channelID, accountID string) bool
}

// Dispatcher signs and delivers outbound events to bots, both via their
// configured event_url (fire-and-forget HTTP) and via in-memory SSE streams
// (socket-mode style). It also intercepts slash commands on the send path.
//
// It implements handlers.BotSink so the spaces handler can stay decoupled.
type Dispatcher struct {
	reg    Registry
	spaces Spaces
	client *http.Client

	mu     sync.Mutex
	nextID int
	subs   map[string]map[int]chan []byte // botID → subId → channel
}

// NewDispatcher builds a dispatcher over a registry and the spaces view used for
// channel-visibility checks. spaces may be nil (no message.created fan-out then).
func NewDispatcher(reg Registry, spaces Spaces) *Dispatcher {
	return &Dispatcher{
		reg:    reg,
		spaces: spaces,
		client: &http.Client{Timeout: 5 * time.Second},
		subs:   make(map[string]map[int]chan []byte),
	}
}

// ---- channel visibility ------------------------------------------------------

// canBotSeeChannel reports whether a bot may receive events for a channel:
// public channels are visible to all bots; private/DM channels require the bot
// to be a member (membership account id "bot:<id>").
func (d *Dispatcher) canBotSeeChannel(b *Bot, channelID string) bool {
	if d.spaces == nil {
		return false
	}
	ch, ok := d.spaces.GetChannel(channelID)
	if !ok {
		return false
	}
	switch ch.Type {
	case models.ChannelTypePrivate, models.ChannelTypeDM:
		return d.spaces.IsMember(channelID, b.AccountID())
	default:
		return true
	}
}

// ---- BotSink: send-path hooks ------------------------------------------------

// OnMessageCreated fans a new message out to every bot that can see the channel
// as a message.created event, plus an app_mention event to mentioned bots. A
// bot never receives its own messages.
func (d *Dispatcher) OnMessageCreated(channelID, msgID, authorID, text, threadParent string) {
	bots, err := d.reg.List("", true) // all bots
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"channel_id":    channelID,
		"message_id":    msgID,
		"author_id":     authorID,
		"text":          text,
		"thread_parent": threadParent,
	}
	for _, b := range bots {
		if authorID == b.AccountID() {
			continue // don't echo a bot its own message
		}
		if !d.canBotSeeChannel(b, channelID) {
			continue
		}
		d.deliver(b, Event{Type: EventMessageCreated, Event: payload})
		if b.Mentions(text) {
			d.deliver(b, Event{Type: EventAppMention, Event: payload})
		}
	}
}

// OnMemberJoined notifies bots in the channel that account joined.
func (d *Dispatcher) OnMemberJoined(channelID, accountID string) {
	bots, err := d.reg.List("", true)
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"channel_id": channelID,
		"account_id": accountID,
	}
	for _, b := range bots {
		if accountID == b.AccountID() {
			continue
		}
		if !d.canBotSeeChannel(b, channelID) {
			continue
		}
		d.deliver(b, Event{Type: EventMemberJoined, Event: payload})
	}
}

// MaybeHandleSlash intercepts a message body that is a registered slash command.
// It returns true when the body was dispatched as a slash command (and thus must
// NOT be stored as a normal message). Unknown commands (or non-slash bodies)
// return false so the caller stores them normally.
func (d *Dispatcher) MaybeHandleSlash(channelID, userID, body string) bool {
	name, args, ok := ParseSlash(body)
	if !ok {
		return false
	}
	bot, cmd, found := d.reg.ResolveSlashCommand(name)
	if !found {
		return false
	}
	d.DispatchSlash(bot, cmd, channelID, userID, args)
	return true
}

// DispatchSlash emits a slash_command event for the resolved command.
func (d *Dispatcher) DispatchSlash(bot *Bot, cmd *SlashCommand, channelID, userID, args string) {
	d.deliver(bot, Event{
		Type: EventSlashCommand,
		Event: map[string]interface{}{
			"command":    cmd.Name,
			"text":       args,
			"channel_id": channelID,
			"user_id":    userID,
		},
	})
}

// ParseSlash splits a "/name args..." body. ok is false when body does not start
// with a slash or has no command token. name is returned without the slash.
func ParseSlash(body string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	rest := strings.TrimPrefix(trimmed, "/")
	if rest == "" {
		return "", "", false
	}
	parts := strings.SplitN(rest, " ", 2)
	name = strings.ToLower(strings.TrimSpace(parts[0]))
	if name == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args, true
}

// ---- delivery ----------------------------------------------------------------

// deliver fills in the envelope metadata and ships the event to the bot's
// event_url (if set) and any live SSE subscribers. Never blocks the caller.
func (d *Dispatcher) deliver(b *Bot, ev Event) {
	ev.BotID = b.ID
	ev.Team = "vulos"
	if ev.EventTime == 0 {
		ev.EventTime = time.Now().Unix()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	d.fanoutSSE(b.ID, body)
	if b.EventURL != "" {
		go d.post(b.EventURL, b.SigningSecret, body)
	}
}

// post signs and POSTs a single event body to url. Best-effort: failures are
// logged, never retried, and never surfaced to the originating request.
func (d *Dispatcher) post(url, secret string, body []byte) {
	ts := NowTimestamp()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[bots] build event request to %s: %v", url, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SigHeaderTimestamp, ts)
	req.Header.Set(SigHeaderSignature, Sign(ts, body, secret))
	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("[bots] deliver event to %s failed: %v", url, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[bots] deliver event to %s: status %d", url, resp.StatusCode)
	}
}

// ---- SSE subscriber registry -------------------------------------------------

// Subscribe registers an SSE subscriber for botID. It returns a receive channel
// for serialized event JSON and an unsubscribe func that must be called on
// disconnect to free the slot.
func (d *Dispatcher) Subscribe(botID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	if d.subs[botID] == nil {
		d.subs[botID] = make(map[int]chan []byte)
	}
	d.subs[botID][id] = ch
	d.mu.Unlock()

	return ch, func() {
		d.mu.Lock()
		if m := d.subs[botID]; m != nil {
			if c, ok := m[id]; ok {
				delete(m, id)
				close(c)
			}
			if len(m) == 0 {
				delete(d.subs, botID)
			}
		}
		d.mu.Unlock()
	}
}

// fanoutSSE pushes body to every live subscriber for botID. Slow/full
// subscribers are skipped (non-blocking send) so one stalled consumer never
// blocks delivery to others or to the originating request.
func (d *Dispatcher) fanoutSSE(botID string, body []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ch := range d.subs[botID] {
		select {
		case ch <- body:
		default:
			// Subscriber is not keeping up; drop this event for them.
		}
	}
}
