package bots

import (
	"encoding/json"
	"testing"
	"time"

	"vulos-talk/backend/models"
)

// fakeSpaces is a minimal bots.Spaces for dispatcher tests.
type fakeSpaces struct {
	channels map[string]*models.Channel
	members  map[string]map[string]bool // channelID → accountID → member
}

func (f *fakeSpaces) GetChannel(id string) (*models.Channel, bool) {
	ch, ok := f.channels[id]
	return ch, ok
}
func (f *fakeSpaces) IsMember(channelID, accountID string) bool {
	return f.members[channelID] != nil && f.members[channelID][accountID]
}

func TestMaybeHandleSlash(t *testing.T) {
	r := NewMemoryRegistry()
	_, _ = r.Create(CreateParams{Name: "ci", OwnerID: "a", SlashCommands: []SlashCommand{{Name: "deploy"}}})
	d := NewDispatcher(r, nil)

	if !d.MaybeHandleSlash("general", "alice", "/deploy prod now") {
		t.Fatalf("registered command should be intercepted")
	}
	if d.MaybeHandleSlash("general", "alice", "/unknown thing") {
		t.Fatalf("unknown command should pass through")
	}
	if d.MaybeHandleSlash("general", "alice", "just a normal message") {
		t.Fatalf("non-slash body should pass through")
	}
}

func TestParseSlash(t *testing.T) {
	cases := []struct {
		body, name, args string
		ok               bool
	}{
		{"/deploy prod", "deploy", "prod", true},
		{"  /Deploy   here  ", "deploy", "here", true},
		{"/just", "just", "", true},
		{"hello world", "", "", false},
		{"/", "", "", false},
	}
	for _, tc := range cases {
		name, args, ok := ParseSlash(tc.body)
		if ok != tc.ok || name != tc.name || args != tc.args {
			t.Fatalf("ParseSlash(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.body, name, args, ok, tc.name, tc.args, tc.ok)
		}
	}
}

func TestSSEFanoutAndOwnMessageSkip(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{Name: "watcher", OwnerID: "a", Scopes: []string{ScopeChatWrite}})
	sp := &fakeSpaces{
		channels: map[string]*models.Channel{"general": {ID: "general", Type: models.ChannelTypePublic}},
	}
	d := NewDispatcher(r, sp)

	events, cancel := d.Subscribe(created.Bot.ID)
	defer cancel()

	// A message from a human is delivered.
	d.OnMessageCreated("general", "m1", "alice", "hello", "")
	select {
	case raw := <-events:
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.Type != EventMessageCreated || ev.BotID != created.Bot.ID || ev.Team != "vulos" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected a message.created event")
	}

	// The bot's OWN message is never echoed back to it.
	d.OnMessageCreated("general", "m2", created.Bot.AccountID(), "from me", "")
	select {
	case raw := <-events:
		t.Fatalf("bot received its own message: %s", raw)
	case <-time.After(150 * time.Millisecond):
		// expected: nothing delivered
	}
}

func TestAppMentionDelivered(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{Name: "helper", OwnerID: "a"})
	sp := &fakeSpaces{channels: map[string]*models.Channel{"general": {ID: "general", Type: models.ChannelTypePublic}}}
	d := NewDispatcher(r, sp)
	events, cancel := d.Subscribe(created.Bot.ID)
	defer cancel()

	d.OnMessageCreated("general", "m1", "alice", "hey @helper can you help", "")

	var types []string
	timeout := time.After(time.Second)
	for len(types) < 2 {
		select {
		case raw := <-events:
			var ev Event
			_ = json.Unmarshal(raw, &ev)
			types = append(types, ev.Type)
		case <-timeout:
			t.Fatalf("expected message.created + app_mention, got %v", types)
		}
	}
	if !(contains(types, EventMessageCreated) && contains(types, EventAppMention)) {
		t.Fatalf("expected both message.created and app_mention, got %v", types)
	}
}

func TestPrivateChannelNotVisibleToNonMemberBot(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{Name: "nosy", OwnerID: "a"})
	sp := &fakeSpaces{
		channels: map[string]*models.Channel{"secret": {ID: "secret", Type: models.ChannelTypePrivate}},
		members:  map[string]map[string]bool{},
	}
	d := NewDispatcher(r, sp)
	events, cancel := d.Subscribe(created.Bot.ID)
	defer cancel()

	d.OnMessageCreated("secret", "m1", "alice", "classified", "")
	select {
	case raw := <-events:
		t.Fatalf("non-member bot received a private-channel event: %s", raw)
	case <-time.After(150 * time.Millisecond):
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
