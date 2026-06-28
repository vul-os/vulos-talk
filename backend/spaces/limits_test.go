package spaces_test

// limits_test.go — body-size hardening on the local write path (DoS bound).

import (
	"errors"
	"strings"
	"testing"

	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"
)

func TestSendMessage_RejectsOversizedBody(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("general", models.ChannelTypePublic, "system")

	// At the cap: accepted.
	atCap := strings.Repeat("a", spaces.MaxMessageBytes)
	if _, err := s.SendMessage(ch.ID, "alice", atCap, ""); err != nil {
		t.Fatalf("body at the cap should be accepted: %v", err)
	}

	// One byte over: rejected with the sentinel.
	over := strings.Repeat("a", spaces.MaxMessageBytes+1)
	_, err := s.SendMessage(ch.ID, "alice", over, "")
	if !errors.Is(err, spaces.ErrMessageTooLarge) {
		t.Fatalf("expected ErrMessageTooLarge, got %v", err)
	}

	// The oversized body must not have landed in the index.
	for _, m := range s.ListMessages(ch.ID) {
		if len(m.Body) > spaces.MaxMessageBytes {
			t.Fatal("oversized message leaked into the store")
		}
	}
}

func TestEditMessage_RejectsOversizedBody(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("general", models.ChannelTypePublic, "system")
	msg, err := s.SendMessage(ch.ID, "alice", "small", "")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	over := strings.Repeat("b", spaces.MaxMessageBytes+1)
	if _, err := s.EditMessage(ch.ID, msg.ID, over); !errors.Is(err, spaces.ErrMessageTooLarge) {
		t.Fatalf("expected ErrMessageTooLarge on edit, got %v", err)
	}
	// Original body must be intact (edit rejected, not partially applied).
	got, _ := s.GetMessage(ch.ID, msg.ID)
	if got.Body != "small" {
		t.Fatalf("body mutated by a rejected oversized edit: %q", got.Body)
	}
}
