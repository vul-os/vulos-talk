package spaces_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"vulos-talk/backend/models"
	"vulos-talk/backend/spaces"

	"github.com/jackc/pgx/v5/pgxpool"
)

// openFn returns a fresh Persister bound to the SAME durable backing store
// (same file / same database) plus a close func. Calling it twice simulates a
// process restart against persisted state.
type openFn func(t *testing.T) (spaces.Persister, func())

// runPersisterContract exercises the durable Persister contract end-to-end
// through the public SpacesStore API: write state in "session 1", reopen the
// backing store in "session 2", and assert everything survived. It also covers
// presence (status/reactions/pins) and, when the Persister implements Searcher,
// full-text search. Both the SQLite and Postgres backends run this identical
// contract so the two stores are behaviourally interchangeable.
func runPersisterContract(t *testing.T, open openFn) {
	t.Helper()

	// --- session 1: write some state, then close ---
	p1, close1 := open(t)
	s1, err := spaces.Open("node-A", p1)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	ch, err := s1.CreateChannel("general", models.ChannelTypePublic, "alice")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := s1.AddMemberWithName(ch.ID, "alice", "Alice A"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	msg, err := s1.SendMessage(ch.ID, "alice", "deploy the release now", "")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := s1.SendMessage(ch.ID, "alice", "totally unrelated chatter", ""); err != nil {
		t.Fatalf("send 2: %v", err)
	}
	if err := s1.MarkRead("alice", ch.ID, msg.SeqClock); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	// Presence: status, reaction, pin — all durable.
	if err := p1.SaveStatus(&models.UserStatus{UserID: "alice", Status: "busy", CustomText: "heads down"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	if err := p1.SaveReaction(&models.Reaction{MessageID: msg.ID, Emoji: "👍", UserID: "alice"}); err != nil {
		t.Fatalf("save reaction: %v", err)
	}
	if err := p1.SavePin(&models.PinnedMessage{ChannelID: ch.ID, MessageID: msg.ID, AuthorID: "alice", Body: msg.Body, PinnedBy: "alice"}); err != nil {
		t.Fatalf("save pin: %v", err)
	}
	close1()

	// --- session 2: reopen the same backing store and verify state survived ---
	p2, close2 := open(t)
	defer close2()
	s2, err := spaces.Open("node-A", p2)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	chs := s2.ListChannels()
	if len(chs) != 1 {
		t.Fatalf("expected 1 channel after restart, got %d", len(chs))
	}
	if chs[0].ID != ch.ID || chs[0].Name != "general" {
		t.Fatalf("channel mismatch after restart: %+v", chs[0])
	}
	if !s2.IsMember(ch.ID, "alice") {
		t.Fatal("membership did not survive restart")
	}
	members := s2.ListMembers(ch.ID)
	if len(members) != 1 || members[0].DisplayName != "Alice A" {
		t.Fatalf("display name did not survive restart: %+v", members)
	}

	msgs := s2.ListMessages(ch.ID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after restart, got %d", len(msgs))
	}

	rs, err := s2.GetReadState("alice", ch.ID)
	if err != nil {
		t.Fatalf("get read state: %v", err)
	}
	if rs.LastReadClock != msg.SeqClock {
		t.Fatalf("read state did not survive: got %q want %q", rs.LastReadClock, msg.SeqClock)
	}

	ops, err := s2.ExportOps(ch.ID, "")
	if err != nil {
		t.Fatalf("export ops: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops after restart, got %d", len(ops))
	}

	// Presence survived.
	statuses, err := p2.ListStatuses()
	if err != nil || len(statuses) != 1 || statuses[0].Status != "busy" {
		t.Fatalf("status did not survive restart: %+v err=%v", statuses, err)
	}
	reactions, err := p2.ListReactions()
	if err != nil || len(reactions) != 1 || reactions[0].MessageID != msg.ID {
		t.Fatalf("reaction did not survive restart: %+v err=%v", reactions, err)
	}
	pins, err := p2.ListPins()
	if err != nil || len(pins) != 1 || pins[0].MessageID != msg.ID {
		t.Fatalf("pin did not survive restart: %+v err=%v", pins, err)
	}

	// Full-text search (optional Searcher capability). Prefix match: "deploy"
	// must match the body "deploy the release now" and only that message.
	if srch, ok := p2.(spaces.Searcher); ok {
		ids, err := srch.SearchMessages(ch.ID, []string{"deploy"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(ids) != 1 || ids[0] != msg.ID {
			t.Fatalf("FTS prefix search mismatch: got %v want [%s]", ids, msg.ID)
		}
		// A term present in neither message returns nothing.
		none, err := srch.SearchMessages(ch.ID, []string{"nonexistentword"})
		if err != nil {
			t.Fatalf("search miss: %v", err)
		}
		if len(none) != 0 {
			t.Fatalf("expected no FTS hits, got %v", none)
		}
	}
}

// TestSQLitePersisterContract runs the durable contract against the embedded
// SQLite backend (the default, always on).
func TestSQLitePersisterContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.db")
	runPersisterContract(t, func(t *testing.T) (spaces.Persister, func()) {
		p, err := spaces.NewSQLitePersister(path)
		if err != nil {
			t.Fatalf("open sqlite persister: %v", err)
		}
		return p, func() { _ = p.Close() }
	})
}

// TestPostgresPersisterContract runs the IDENTICAL contract against the Postgres
// backend (schema `talk`). It is SKIPPED unless VULOS_TEST_POSTGRES points at a
// throwaway database, so CI without Postgres still passes; the postgres-path CI
// job sets it. DATABASE_URL is accepted as a fallback so the same DSN that
// drives the runtime can drive the test.
func TestPostgresPersisterContract(t *testing.T) {
	dsn := os.Getenv("VULOS_TEST_POSTGRES")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set VULOS_TEST_POSTGRES to run the Postgres spaces contract test")
	}

	// Clean slate: drop the talk schema so the contract's row-count assertions
	// are deterministic across reruns. The persister recreates it on open.
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS talk CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("reset schema: %v", err)
	}
	pool.Close()

	runPersisterContract(t, func(t *testing.T) (spaces.Persister, func()) {
		p, err := spaces.NewPostgresPersister(dsn)
		if err != nil {
			t.Fatalf("open postgres persister: %v", err)
		}
		return p, func() { _ = p.Close() }
	})
}
