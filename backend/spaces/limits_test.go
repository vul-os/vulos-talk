package spaces_test

// limits_test.go — body-size hardening on the local write path (DoS bound)
// and the CRDT merge path (op-batch and per-op body cap).

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

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

// TestMergeOpsAs_RejectsOversizedOpBody verifies that an op whose body exceeds
// MaxMessageBytes is rejected by MergeOpsAs (the CRDT merge path), closing the
// DoS bypass where an attacker routes an oversized payload through op-merge
// instead of the normal send/edit endpoints.
func TestMergeOpsAs_RejectsOversizedOpBody(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("general", models.ChannelTypePublic, "alice")
	_, _ = s.AddMember(ch.ID, "alice")

	oversizedBody := strings.Repeat("X", spaces.MaxMessageBytes+1)
	op := &models.MessageOp{
		Op:        models.MessageOpAppend,
		ChannelID: ch.ID,
		Msg: models.Message{
			ID:        uuid.NewString(),
			ChannelID: ch.ID,
			AuthorID:  "alice",
			Body:      oversizedBody,
			State:     models.MessageStateActive,
			SeqClock:  "00000000000000000001-0000000000-test",
		},
	}
	err := s.MergeOpsAs("alice", []*models.MessageOp{op})
	if !errors.Is(err, spaces.ErrMessageTooLarge) {
		t.Fatalf("expected ErrMessageTooLarge from MergeOpsAs, got: %v", err)
	}
	// The oversized op must not have landed in the index.
	for _, m := range s.ListMessages(ch.ID) {
		if len(m.Body) > spaces.MaxMessageBytes {
			t.Fatal("oversized op body leaked into the store via MergeOpsAs")
		}
	}
}

// TestMergeOpsAs_RejectsOversizedBatch verifies that a batch containing more
// than MaxMergeOpsPerBatch ops is rejected atomically before any op is applied.
func TestMergeOpsAs_RejectsOversizedBatch(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("general", models.ChannelTypePublic, "alice")
	_, _ = s.AddMember(ch.ID, "alice")

	ops := make([]*models.MessageOp, spaces.MaxMergeOpsPerBatch+1)
	for i := range ops {
		ops[i] = &models.MessageOp{
			Op:        models.MessageOpAppend,
			ChannelID: ch.ID,
			Msg: models.Message{
				ID:        uuid.NewString(),
				ChannelID: ch.ID,
				AuthorID:  "alice",
				Body:      "x",
				State:     models.MessageStateActive,
				SeqClock:  "00000000000000000001-0000000000-test",
			},
		}
	}
	err := s.MergeOpsAs("alice", ops)
	if !errors.Is(err, spaces.ErrBatchTooLarge) {
		t.Fatalf("expected ErrBatchTooLarge from MergeOpsAs, got: %v", err)
	}
	// No message must have been applied.
	if msgs := s.ListMessages(ch.ID); len(msgs) != 0 {
		t.Fatalf("batch was partially applied despite rejection: %d messages in store", len(msgs))
	}
}
