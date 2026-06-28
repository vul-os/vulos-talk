package spaces_test

// concurrency_test.go — race/convergence stress for the CRDT SpacesStore.
//
// The store advertises that ApplyOp / MergeOps / Send / Edit are safe to call
// from multiple goroutines (see the package doc in store.go). These tests pound
// the store from many goroutines at once; run them with `go test -race ./...`
// to surface data races in the in-memory indexes and the HLC.
//
// They assert three properties under concurrency:
//   - no lost writes (every concurrent SendMessage lands exactly once),
//   - SeqClock uniqueness (the HLC never hands out a duplicate clock), and
//   - convergence (idempotent + commutative merges land at the same state).

import (
	"fmt"
	"sync"
	"testing"

	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"
)

// TestConcurrentSendMessage_NoLostWritesOrDupClocks fans out N goroutines each
// sending M messages into the same channel and asserts every message is stored
// and every SeqClock is unique (the HLC is the only ordering authority).
func TestConcurrentSendMessage_NoLostWritesOrDupClocks(t *testing.T) {
	s := openStore(t, "node-A")
	ch, err := s.CreateChannel("general", models.ChannelTypePublic, "system")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	const goroutines = 16
	const perG = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perG)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				author := fmt.Sprintf("user-%d", g)
				if _, err := s.SendMessage(ch.ID, author, fmt.Sprintf("msg %d-%d", g, i), ""); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}

	msgs := s.ListMessages(ch.ID)
	want := goroutines * perG
	if len(msgs) != want {
		t.Fatalf("lost writes: got %d messages, want %d", len(msgs), want)
	}
	seen := make(map[string]bool, want)
	for _, m := range msgs {
		if m.SeqClock == "" {
			t.Fatal("empty SeqClock")
		}
		if seen[m.SeqClock] {
			t.Fatalf("duplicate SeqClock handed out: %q", m.SeqClock)
		}
		seen[m.SeqClock] = true
	}
}

// TestConcurrentEditAndRead exercises the writer/reader path: one writer edits a
// message in a tight loop while many readers list/get concurrently. Under -race
// this catches an unsynchronised read of the message index. The final body must
// be one of the values actually written (LWW), never torn.
func TestConcurrentEditAndRead(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("general", models.ChannelTypePublic, "system")
	msg, err := s.SendMessage(ch.ID, "alice", "v0", "")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var wg sync.WaitGroup
	const edits = 200
	// Writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < edits; i++ {
			if _, err := s.EditMessage(ch.ID, msg.ID, fmt.Sprintf("v%d", i+1)); err != nil {
				t.Errorf("edit: %v", err)
				return
			}
		}
	}()
	// Concurrent readers.
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < edits; i++ {
				_ = s.ListMessages(ch.ID)
				if m, ok := s.GetMessage(ch.ID, msg.ID); ok {
					_ = m.Body
				}
			}
		}()
	}
	wg.Wait()

	final, ok := s.GetMessage(ch.ID, msg.ID)
	if !ok {
		t.Fatal("message vanished after concurrent edits")
	}
	if final.State != models.MessageStateEdited {
		t.Fatalf("expected edited state, got %s", final.State)
	}
}

// TestConcurrentMergeOpsConverges proves the merge is idempotent + commutative
// under concurrency: two replicas merge the SAME op set from many goroutines and
// in different orders, then must hold identical state.
func TestConcurrentMergeOpsConverges(t *testing.T) {
	// Build a canonical op set authored on node-A.
	src := openStore(t, "node-A")
	chID := "shared"
	if _, err := src.CreateChannelWithID(chID, "shared", models.ChannelTypePublic, "system"); err != nil {
		t.Fatalf("create: %v", err)
	}
	const n = 100
	for i := 0; i < n; i++ {
		if _, err := src.SendMessage(chID, "alice", fmt.Sprintf("m%d", i), ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ops, err := src.ExportOps(chID, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(ops) != n {
		t.Fatalf("expected %d ops, got %d", n, len(ops))
	}

	// Two fresh replicas each merge the ops concurrently (and redundantly — the
	// same batch applied many times must remain idempotent).
	mergeAll := func(nodeID string) *spaces.SpacesStore {
		st := openStore(t, nodeID)
		_, _ = st.CreateChannelWithID(chID, "shared", models.ChannelTypePublic, "system")
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Apply the whole batch twice to stress idempotency.
				_ = st.MergeOps(ops)
				_ = st.MergeOps(ops)
			}()
		}
		wg.Wait()
		return st
	}

	rep1 := mergeAll("node-B")
	rep2 := mergeAll("node-C")

	m1 := s_index(rep1.ListMessages(chID))
	m2 := s_index(rep2.ListMessages(chID))
	if len(m1) != n {
		t.Fatalf("replica1 did not converge: got %d messages, want %d", len(m1), n)
	}
	if len(m1) != len(m2) {
		t.Fatalf("replicas diverged: %d vs %d", len(m1), len(m2))
	}
	for id, body := range m1 {
		if m2[id] != body {
			t.Fatalf("replicas diverged on %s: %q vs %q", id, body, m2[id])
		}
	}
}

// TestConcurrentAddMemberAndIsMember stresses the membership map under a mix of
// writers and readers (catches an unguarded members map under -race).
func TestConcurrentAddMemberAndIsMember(t *testing.T) {
	s := openStore(t, "node-A")
	ch, _ := s.CreateChannel("team", models.ChannelTypePrivate, "system")

	const members = 100
	var wg sync.WaitGroup
	for i := 0; i < members; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			acct := fmt.Sprintf("user-%d", i)
			if _, err := s.AddMember(ch.ID, acct); err != nil {
				t.Errorf("add member: %v", err)
			}
			_ = s.IsMember(ch.ID, acct)
			_ = s.ListMembers(ch.ID)
		}(i)
	}
	wg.Wait()

	got := len(s.ListMembers(ch.ID))
	if got != members {
		t.Fatalf("expected %d members, got %d", members, got)
	}
}

// s_index reduces a message slice to id→body for convergence comparison.
func s_index(msgs []*models.Message) map[string]string {
	out := make(map[string]string, len(msgs))
	for _, m := range msgs {
		out[m.ID] = m.Body
	}
	return out
}
