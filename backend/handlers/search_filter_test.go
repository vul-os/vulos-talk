package handlers

// search_filter_test.go — white-box unit tests for the search query parser and
// matcher (parseSearchFilter / matchMsg), plus a REST test of the operator path
// over the in-memory fallback scan (NullPersister is not a Searcher, so this
// exercises the non-FTS branch the SQLite FTS tests don't reach).

import (
	"net/http"
	"testing"
	"time"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
)

func TestParseSearchFilter_Operators(t *testing.T) {
	f := parseSearchFilter("hello from:alice before:2026-01-01 after:2025-01-01 has:link has:file world")
	if len(f.terms) != 2 || f.terms[0] != "hello" || f.terms[1] != "world" {
		t.Fatalf("terms parse: %+v", f.terms)
	}
	if f.from != "alice" {
		t.Fatalf("from=%q", f.from)
	}
	if !f.hasBefore || !f.hasAfter {
		t.Fatalf("date flags: before=%v after=%v", f.hasBefore, f.hasAfter)
	}
	if !f.hasLink || !f.hasFile {
		t.Fatalf("has flags: link=%v file=%v", f.hasLink, f.hasFile)
	}
}

func TestParseSearchFilter_BadDateIgnored(t *testing.T) {
	f := parseSearchFilter("before:not-a-date term")
	if f.hasBefore {
		t.Fatal("malformed before: date should be ignored, not panic or flag")
	}
	if len(f.terms) != 1 || f.terms[0] != "term" {
		t.Fatalf("terms=%+v", f.terms)
	}
}

func TestMatchMsg_TermAndFrom(t *testing.T) {
	m := &models.Message{Body: "Deploy the service", AuthorID: "alice"}
	if !matchMsg(m, parseSearchFilter("deploy")) {
		t.Fatal("term should match (case-insensitive)")
	}
	if matchMsg(m, parseSearchFilter("missing")) {
		t.Fatal("non-present term must not match")
	}
	if !matchMsg(m, parseSearchFilter("from:alice")) {
		t.Fatal("from:alice should match author")
	}
	if matchMsg(m, parseSearchFilter("from:bob")) {
		t.Fatal("from:bob must not match alice's message")
	}
}

func TestMatchMsg_HasLinkAndFile(t *testing.T) {
	link := &models.Message{Body: "see https://example.com", AuthorID: "x"}
	file := &models.Message{Body: "[file:report.pdf]", AuthorID: "x"}
	plain := &models.Message{Body: "just text", AuthorID: "x"}

	if !matchMsg(link, parseSearchFilter("has:link")) {
		t.Fatal("has:link should match a URL body")
	}
	if matchMsg(plain, parseSearchFilter("has:link")) {
		t.Fatal("has:link must not match plain text")
	}
	if !matchMsg(file, parseSearchFilter("has:file")) {
		t.Fatal("has:file should match a [file:...] body")
	}
	if matchMsg(plain, parseSearchFilter("has:file")) {
		t.Fatal("has:file must not match plain text")
	}
}

func TestMatchMsg_DateBounds(t *testing.T) {
	mid := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	m := &models.Message{Body: "x", AuthorID: "a", CreatedAt: mid}
	if !matchMsg(m, parseSearchFilter("after:2025-01-01")) {
		t.Fatal("message after the bound should match")
	}
	if matchMsg(m, parseSearchFilter("after:2025-12-01")) {
		t.Fatal("message before the after-bound must not match")
	}
	if !matchMsg(m, parseSearchFilter("before:2025-12-01")) {
		t.Fatal("message before the bound should match")
	}
	if matchMsg(m, parseSearchFilter("before:2025-01-01")) {
		t.Fatal("message after the before-bound must not match")
	}
}

// TestSearchMessages_REST_FallbackScan exercises the operator path end-to-end
// over the NullPersister (non-FTS) fallback, including the tombstone exclusion.
func TestSearchMessages_REST_FallbackScan(t *testing.T) {
	h := testHandler(t)
	ch, _ := h.store.CreateChannel("room", models.ChannelTypePublic, "alice")
	_, _ = h.store.AddMember(ch.ID, "alice")
	keep, _ := h.store.SendMessage(ch.ID, "alice", "deploy production now", "")
	_, _ = h.store.SendMessage(ch.ID, "bob", "deploy staging", "")
	gone, _ := h.store.SendMessage(ch.ID, "alice", "deploy secret", "")
	_ = h.store.DeleteMessage(ch.ID, gone.ID) // tombstoned → excluded from search

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, "alice")
		c.Next()
	})
	r.GET("/spaces/channels/:channelId/search", h.SearchMessages)

	// term + from: operator narrows to alice's surviving "deploy" message.
	w := doReq(r, http.MethodGet, "/spaces/channels/"+ch.ID+"/search?q=deploy+from:alice", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("search: %d (%s)", w.Code, w.Body.String())
	}
	var got []*models.Message
	mustDecode(t, w, &got)
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("expected only alice's live message %q, got %+v", keep.ID, got)
	}
	// The tombstoned message must never surface.
	for _, m := range got {
		if m.ID == gone.ID {
			t.Fatal("VULN: tombstoned message returned in search results")
		}
	}
}
