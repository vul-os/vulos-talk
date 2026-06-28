package handlers

// spaces_lifecycle_test.go — REST-level coverage for the message lifecycle
// (edit/delete author-gating), read-state, reactions, and pins routes. These
// complement the store-level CRDT tests and the authz pentests by proving the
// HTTP handlers enforce ownership and membership and shape responses correctly.

import (
	"net/http"
	"testing"

	"vulos-talk/backend/middleware"
	"vulos-talk/backend/models"

	"github.com/gin-gonic/gin"
)

// lifecycleRouter wires the full message-lifecycle + presence surface with an
// injected verified identity (and optional admin scope).
func lifecycleRouter(h *SpacesHandlerExt, user string, admin bool) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxAuthenticated, true)
		c.Set(middleware.CtxUserID, user)
		if admin {
			c.Set(middleware.CtxIsAdmin, true)
		}
		c.Next()
	})
	r.GET("/spaces/channels/:channelId/messages", h.ListMessages)
	r.PUT("/spaces/channels/:channelId/messages/:msgId", h.EditMessage)
	r.DELETE("/spaces/channels/:channelId/messages/:msgId", h.DeleteMessage)
	r.POST("/spaces/channels/:channelId/read", h.MarkRead)
	r.GET("/spaces/channels/:channelId/read", h.GetReadState)
	r.GET("/spaces/channels/:channelId/reactions", h.ListReactions)
	r.POST("/spaces/messages/:msgId/react", h.React)
	r.DELETE("/spaces/messages/:msgId/react", h.Unreact)
	r.GET("/spaces/channels/:channelId/pins", h.ListPins)
	r.POST("/spaces/channels/:channelId/pins", h.PinMessage)
	r.DELETE("/spaces/channels/:channelId/pins/:msgId", h.UnpinMessage)
	r.PUT("/spaces/users/me/status", h.SetStatus)
	return r
}

// seedPublicMsg creates a public channel (members alice+bob) and a message by
// alice; returns (channelID, msgID).
func seedPublicMsg(t *testing.T, h *SpacesHandlerExt) (string, string) {
	t.Helper()
	ch, err := h.store.CreateChannel("room", models.ChannelTypePublic, "alice")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	_, _ = h.store.AddMember(ch.ID, "alice")
	_, _ = h.store.AddMember(ch.ID, "bob")
	msg, err := h.store.SendMessage(ch.ID, "alice", "alice's message", "")
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	return ch.ID, msg.ID
}

// ---- Edit -------------------------------------------------------------------

func TestEditMessage_AuthorCanEdit(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPut, "/spaces/channels/"+chID+"/messages/"+msgID,
		models.EditMessageRequest{Body: "edited by alice"})
	if w.Code != http.StatusOK {
		t.Fatalf("author edit: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got, _ := h.store.GetMessage(chID, msgID)
	if got.Body != "edited by alice" {
		t.Fatalf("body not updated: %q", got.Body)
	}
}

func TestEditMessage_NonAuthorMemberDenied(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	// bob is a member of the (public) channel but did NOT author the message.
	bob := lifecycleRouter(h, "bob", false)
	w := doReq(bob, http.MethodPut, "/spaces/channels/"+chID+"/messages/"+msgID,
		models.EditMessageRequest{Body: "tampered by bob"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("VULN: non-author edited another user's message — got %d (want 403)", w.Code)
	}
	got, _ := h.store.GetMessage(chID, msgID)
	if got.Body != "alice's message" {
		t.Fatalf("VULN: message body changed by non-author: %q", got.Body)
	}
}

func TestEditMessage_AdminCanEdit(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	admin := lifecycleRouter(h, "root", true)
	w := doReq(admin, http.MethodPut, "/spaces/channels/"+chID+"/messages/"+msgID,
		models.EditMessageRequest{Body: "moderated"})
	if w.Code != http.StatusOK {
		t.Fatalf("admin edit: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestEditMessage_MissingMessage404(t *testing.T) {
	h := testHandler(t)
	chID, _ := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPut, "/spaces/channels/"+chID+"/messages/does-not-exist",
		models.EditMessageRequest{Body: "x"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing message, got %d", w.Code)
	}
}

func TestEditMessage_TombstonedRejected(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	if err := h.store.DeleteMessage(chID, msgID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPut, "/spaces/channels/"+chID+"/messages/"+msgID,
		models.EditMessageRequest{Body: "resurrect"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 editing a tombstoned message, got %d (%s)", w.Code, w.Body.String())
	}
}

// ---- Delete -----------------------------------------------------------------

func TestDeleteMessage_AuthorCanDelete(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodDelete, "/spaces/channels/"+chID+"/messages/"+msgID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("author delete: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got, _ := h.store.GetMessage(chID, msgID)
	if got.State != models.MessageStateTombed {
		t.Fatalf("message not tombstoned, state=%s", got.State)
	}
}

func TestDeleteMessage_NonAuthorDenied(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	bob := lifecycleRouter(h, "bob", false)
	w := doReq(bob, http.MethodDelete, "/spaces/channels/"+chID+"/messages/"+msgID, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("VULN: non-author deleted another's message — got %d (want 403)", w.Code)
	}
	got, _ := h.store.GetMessage(chID, msgID)
	if got.State == models.MessageStateTombed {
		t.Fatal("VULN: message tombstoned by a non-author")
	}
}

// ---- Read state -------------------------------------------------------------

func TestReadState_RoundTrip(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	msg, _ := h.store.GetMessage(chID, msgID)
	alice := lifecycleRouter(h, "alice", false)

	w := doReq(alice, http.MethodPost, "/spaces/channels/"+chID+"/read",
		map[string]string{"clock": msg.SeqClock})
	if w.Code != http.StatusOK {
		t.Fatalf("mark read: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	w = doReq(alice, http.MethodGet, "/spaces/channels/"+chID+"/read", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get read state: expected 200, got %d", w.Code)
	}
	var rs models.ReadState
	mustDecode(t, w, &rs)
	if rs.LastReadClock != msg.SeqClock {
		t.Fatalf("read clock not persisted: got %q want %q", rs.LastReadClock, msg.SeqClock)
	}
}

func TestReadState_MissingClock400(t *testing.T) {
	h := testHandler(t)
	chID, _ := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPost, "/spaces/channels/"+chID+"/read", map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing clock, got %d", w.Code)
	}
}

func TestReadState_NonMemberDenied(t *testing.T) {
	h := testHandler(t)
	ch, _ := h.store.CreateChannel("secret", models.ChannelTypePrivate, "alice")
	_, _ = h.store.AddMember(ch.ID, "alice")
	eve := lifecycleRouter(h, "eve", false)
	w := doReq(eve, http.MethodPost, "/spaces/channels/"+ch.ID+"/read", map[string]string{"clock": "x"})
	if w.Code == http.StatusOK {
		t.Fatalf("VULN: non-member wrote read-state into a private channel — got %d", w.Code)
	}
}

// ---- Reactions --------------------------------------------------------------

func TestReact_AddIsIdempotentAndListed(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)

	body := models.ReactRequest{Emoji: "👍", ChannelID: chID}
	for i := 0; i < 3; i++ { // idempotent: 3 identical reactions == 1
		if w := doReq(alice, http.MethodPost, "/spaces/messages/"+msgID+"/react", body); w.Code != http.StatusOK {
			t.Fatalf("react: expected 200, got %d (%s)", w.Code, w.Body.String())
		}
	}
	w := doReq(alice, http.MethodGet, "/spaces/channels/"+chID+"/reactions", nil)
	var rxns []*models.Reaction
	mustDecode(t, w, &rxns)
	count := 0
	for _, r := range rxns {
		if r.MessageID == msgID && r.Emoji == "👍" && r.UserID == "alice" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 reaction after idempotent adds, got %d", count)
	}
}

func TestReact_MissingMessage404(t *testing.T) {
	h := testHandler(t)
	chID, _ := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPost, "/spaces/messages/ghost/react",
		models.ReactRequest{Emoji: "👍", ChannelID: chID})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 reacting to a non-existent message, got %d", w.Code)
	}
}

func TestReact_EmptyEmoji400(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	w := doReq(alice, http.MethodPost, "/spaces/messages/"+msgID+"/react",
		models.ReactRequest{Emoji: "   ", ChannelID: chID})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty emoji, got %d", w.Code)
	}
}

func TestUnreact_OnlyRemovesOwnReaction(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)
	bob := lifecycleRouter(h, "bob", false)

	react := models.ReactRequest{Emoji: "🔥", ChannelID: chID}
	if w := doReq(alice, http.MethodPost, "/spaces/messages/"+msgID+"/react", react); w.Code != http.StatusOK {
		t.Fatalf("alice react: %d", w.Code)
	}
	if w := doReq(bob, http.MethodPost, "/spaces/messages/"+msgID+"/react", react); w.Code != http.StatusOK {
		t.Fatalf("bob react: %d", w.Code)
	}
	// bob unreacts — must remove ONLY bob's reaction, never alice's.
	if w := doReq(bob, http.MethodDelete, "/spaces/messages/"+msgID+"/react", react); w.Code != http.StatusOK {
		t.Fatalf("bob unreact: %d", w.Code)
	}
	w := doReq(alice, http.MethodGet, "/spaces/channels/"+chID+"/reactions", nil)
	var rxns []*models.Reaction
	mustDecode(t, w, &rxns)
	var aliceStill, bobStill bool
	for _, r := range rxns {
		if r.Emoji == "🔥" && r.UserID == "alice" {
			aliceStill = true
		}
		if r.Emoji == "🔥" && r.UserID == "bob" {
			bobStill = true
		}
	}
	if !aliceStill {
		t.Fatal("VULN: bob's unreact removed alice's reaction")
	}
	if bobStill {
		t.Fatal("bob's own reaction was not removed")
	}
}

// ---- Pins -------------------------------------------------------------------

func TestPins_PinListUnpin(t *testing.T) {
	h := testHandler(t)
	chID, msgID := seedPublicMsg(t, h)
	alice := lifecycleRouter(h, "alice", false)

	if w := doReq(alice, http.MethodPost, "/spaces/channels/"+chID+"/pins",
		models.PinRequest{MessageID: msgID}); w.Code != http.StatusCreated {
		t.Fatalf("pin: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	// Pin again — idempotent, still one pin.
	_ = doReq(alice, http.MethodPost, "/spaces/channels/"+chID+"/pins", models.PinRequest{MessageID: msgID})

	w := doReq(alice, http.MethodGet, "/spaces/channels/"+chID+"/pins", nil)
	var pins []*models.PinnedMessage
	mustDecode(t, w, &pins)
	if len(pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(pins))
	}
	if pins[0].Body != "alice's message" || pins[0].AuthorID != "alice" {
		t.Fatalf("pin snapshot wrong: %+v", pins[0])
	}

	if w := doReq(alice, http.MethodDelete, "/spaces/channels/"+chID+"/pins/"+msgID, nil); w.Code != http.StatusOK {
		t.Fatalf("unpin: expected 200, got %d", w.Code)
	}
	w = doReq(alice, http.MethodGet, "/spaces/channels/"+chID+"/pins", nil)
	var after []*models.PinnedMessage
	mustDecode(t, w, &after)
	if len(after) != 0 {
		t.Fatalf("expected 0 pins after unpin, got %d", len(after))
	}
}

// ---- Status -----------------------------------------------------------------

func TestSetStatus_BindsToAuthenticatedUser(t *testing.T) {
	h := testHandler(t)
	alice := lifecycleRouter(h, "alice", false)
	// Even if the body tried to carry a user id, the handler scopes to the JWT.
	w := doReq(alice, http.MethodPut, "/spaces/users/me/status",
		models.SetStatusRequest{Status: "away", CustomText: "lunch"})
	if w.Code != http.StatusOK {
		t.Fatalf("set status: expected 200, got %d", w.Code)
	}
	got := h.ext.status.Get("alice")
	if got.Status != "away" || got.CustomText != "lunch" {
		t.Fatalf("status not stored for the authenticated user: %+v", got)
	}
}
