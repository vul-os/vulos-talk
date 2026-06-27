package handlers

// Talk's adapter onto the shared Vulos Apps & Bots platform
// (github.com/vul-os/vulos-apps/appsplatform).
//
// The platform owns auth, token hashing, product-targeting and scope
// enforcement; this adapter owns Talk-native semantics: it posts chat messages,
// adds/removes reactions, and reads channels/history/members against the
// existing Spaces store, enforcing the SAME channel-membership authz the human
// REST surface uses. An app posts/acts as the synthetic account "app:<id>".
//
// This file (plus the composition root in main.go) are the only places that
// import appsplatform; the Spaces core stays free of it.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"vulos-talk/backend/models"

	"github.com/vul-os/vulos-apps/appsplatform"
)

// Talk action / read kinds carried in the generic platform envelopes. The Talk
// convention is action:"message.post", target:"<channel>", payload:{text}.
const (
	ActMessagePost    = "message.post"
	ActReactionAdd    = "reaction.add"
	ActReactionRemove = "reaction.remove"
	ActIncoming       = "incoming_webhook" // platform's incoming-webhook action

	ReadHistory  = "history"
	ReadChannels = "channels"
	ReadMembers  = "members"
)

// TalkAdapter implements appsplatform.ProductAdapter for Talk.
type TalkAdapter struct {
	spaces *SpacesHandlerExt
}

// NewTalkAdapter builds the adapter over the shared spaces handler.
func NewTalkAdapter(spaces *SpacesHandlerExt) *TalkAdapter { return &TalkAdapter{spaces: spaces} }

// Product identifies this adapter's product.
func (a *TalkAdapter) Product() string { return appsplatform.ProductTalk }

// RequiredScope maps a Talk action/read kind to the scope it needs.
func (a *TalkAdapter) RequiredScope(actionOrKind string) string {
	switch actionOrKind {
	case ActMessagePost:
		return appsplatform.ScopeChatWrite
	case ActReactionAdd, ActReactionRemove:
		return appsplatform.ScopeReactionsWrite
	case ReadHistory:
		return appsplatform.ScopeHistoryRead
	case ReadChannels:
		return appsplatform.ScopeChannelsRead
	case ReadMembers:
		return appsplatform.ScopeMembersRead
	default:
		// auth.test, incoming_webhook (unauthenticated) and unknowns need none.
		return ""
	}
}

// CanAccessTarget enforces channel visibility: public channels are open; private
// / DM channels require the app to be a member (membership id "app:<id>").
func (a *TalkAdapter) CanAccessTarget(app *appsplatform.App, target string) (allowed, exists bool) {
	if strings.TrimSpace(target) == "" {
		return true, true
	}
	return a.spaces.canAccessChannel(target, app.AccountID())
}

// Act performs a Talk-native action for an app at runtime.
func (a *TalkAdapter) Act(ctx context.Context, app *appsplatform.App, req appsplatform.ActionRequest, emit appsplatform.EmitFunc) (any, error) {
	switch req.Action {
	case ActMessagePost, ActIncoming:
		return a.postMessage(app, req, emit)
	case ActReactionAdd:
		return a.react(app, req, true)
	case ActReactionRemove:
		return a.react(app, req, false)
	default:
		return nil, errors.New("unknown action: " + req.Action)
	}
}

// postMessage posts a chat message as the app. The channel comes from the
// target, falling back to a payload channel_id (and, for incoming webhooks, to
// the app's default target / "general"). Channel authz is re-checked here so the
// incoming-webhook path — which the platform invokes WITHOUT a scope/target
// check, the id being the secret — can never post into a private channel the app
// is not a member of.
func (a *TalkAdapter) postMessage(app *appsplatform.App, req appsplatform.ActionRequest, emit appsplatform.EmitFunc) (any, error) {
	var p struct {
		Text         string `json:"text"`
		ThreadParent string `json:"thread_parent"`
		ChannelID    string `json:"channel_id"`
	}
	_ = json.Unmarshal(req.Payload, &p)

	channel := firstNonEmpty(req.Target, p.ChannelID)
	if req.Action == ActIncoming && channel == "" {
		channel = firstNonEmpty(app.DefaultTarget, "general")
	}
	if strings.TrimSpace(channel) == "" {
		return nil, errors.New("target channel required")
	}
	if strings.TrimSpace(p.Text) == "" {
		return nil, errors.New("text required")
	}
	if allowed, exists := a.spaces.canAccessChannel(channel, app.AccountID()); !exists {
		return nil, errors.New("channel not found")
	} else if !allowed {
		return nil, errors.New("app is not a member of this channel")
	}
	msg, err := a.spaces.store.SendMessage(channel, app.AccountID(), p.Text, p.ThreadParent)
	if err != nil {
		return nil, err
	}
	fanoutMessage(emit, a, msg)
	return msg, nil
}

// react adds or removes a reaction authored by the app.
func (a *TalkAdapter) react(app *appsplatform.App, req appsplatform.ActionRequest, add bool) (any, error) {
	var p struct {
		Emoji     string `json:"emoji"`
		MessageID string `json:"message_id"`
		ChannelID string `json:"channel_id"`
	}
	_ = json.Unmarshal(req.Payload, &p)
	channel := firstNonEmpty(req.Target, p.ChannelID)
	if strings.TrimSpace(p.Emoji) == "" || strings.TrimSpace(p.MessageID) == "" {
		return nil, errors.New("message_id and emoji required")
	}
	if allowed, exists := a.spaces.canAccessChannel(channel, app.AccountID()); !exists {
		return nil, errors.New("channel not found")
	} else if !allowed {
		return nil, errors.New("app is not a member of this channel")
	}
	if _, found := a.spaces.store.GetMessage(channel, p.MessageID); !found {
		return nil, errors.New("message not found in channel")
	}
	if add {
		a.spaces.ext.reactions.Add(p.MessageID, p.Emoji, app.AccountID())
	} else {
		a.spaces.ext.reactions.Remove(p.MessageID, p.Emoji, app.AccountID())
	}
	return map[string]any{"ok": true}, nil
}

// Read returns Talk content for an app: channels, history, or members.
func (a *TalkAdapter) Read(ctx context.Context, app *appsplatform.App, req appsplatform.ReadRequest) (any, error) {
	switch req.Kind {
	case ReadChannels:
		out := make([]*models.Channel, 0)
		for _, ch := range a.spaces.store.ListChannels() {
			switch ch.Type {
			case models.ChannelTypePrivate, models.ChannelTypeDM:
				if a.spaces.store.IsMember(ch.ID, app.AccountID()) {
					out = append(out, ch)
				}
			default:
				out = append(out, ch)
			}
		}
		return out, nil
	case ReadHistory:
		limit := 50
		if v := req.Params["limit"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > 200 {
			limit = 200
		}
		msgs := a.spaces.store.ListMessages(req.Target)
		if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
		if msgs == nil {
			msgs = []*models.Message{}
		}
		return msgs, nil
	case ReadMembers:
		members := a.spaces.store.ListMembers(req.Target)
		if members == nil {
			members = []*models.Membership{}
		}
		return members, nil
	default:
		return nil, errors.New("unknown read kind: " + req.Kind)
	}
}

// canAppSeeChannel reports whether app may receive events for a channel: public
// channels are visible to all apps; private/DM channels require membership.
func (a *TalkAdapter) canAppSeeChannel(app *appsplatform.App, channelID string) bool {
	ch, ok := a.spaces.store.GetChannel(channelID)
	if !ok {
		return false
	}
	switch ch.Type {
	case models.ChannelTypePrivate, models.ChannelTypeDM:
		return a.spaces.store.IsMember(channelID, app.AccountID())
	default:
		return true
	}
}

// fanoutMessage emits message.created (to every app that can see the channel and
// is not the author) and app_mention (to mentioned apps). Shared by the runtime
// act path and the send-path sink.
func fanoutMessage(emit appsplatform.EmitFunc, a *TalkAdapter, msg *models.Message) {
	if emit == nil {
		return
	}
	payload := map[string]any{
		"channel_id":    msg.ChannelID,
		"message_id":    msg.ID,
		"author_id":     msg.AuthorID,
		"text":          msg.Body,
		"thread_parent": msg.ThreadParent,
	}
	notSelfAndVisible := func(app *appsplatform.App) bool {
		return app.AccountID() != msg.AuthorID && a.canAppSeeChannel(app, msg.ChannelID)
	}
	emit(appsplatform.EventMessageCreated, payload, notSelfAndVisible)
	emit(appsplatform.EventAppMention, payload, func(app *appsplatform.App) bool {
		return notSelfAndVisible(app) && app.Mentions(msg.Body)
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
