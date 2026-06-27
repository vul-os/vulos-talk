package handlers

// AppsSink bridges the Spaces send/reply/join path to the shared Apps & Bots
// platform dispatcher. It implements handlers.BotSink (so the spaces handler
// stays decoupled and free of any appsplatform import) and fans Talk events out
// to subscribed apps via the platform's product-agnostic Dispatcher.

import (
	"github.com/vul-os/vulos-apps/appsplatform"
)

// AppsSink implements BotSink over the appsplatform dispatcher + registry.
type AppsSink struct {
	reg     appsplatform.Registry
	disp    *appsplatform.Dispatcher
	adapter *TalkAdapter
}

// NewAppsSink wires the sink. The adapter supplies channel-visibility checks so
// only apps that can see a channel receive its events.
func NewAppsSink(reg appsplatform.Registry, disp *appsplatform.Dispatcher, adapter *TalkAdapter) *AppsSink {
	return &AppsSink{reg: reg, disp: disp, adapter: adapter}
}

var _ BotSink = (*AppsSink)(nil)

// OnMessageCreated fans a new message out as message.created (to every app that
// can see the channel and is not the author) plus app_mention to mentioned apps.
func (s *AppsSink) OnMessageCreated(channelID, msgID, authorID, text, threadParent string) {
	payload := map[string]any{
		"channel_id":    channelID,
		"message_id":    msgID,
		"author_id":     authorID,
		"text":          text,
		"thread_parent": threadParent,
	}
	notSelfAndVisible := func(app *appsplatform.App) bool {
		return app.AccountID() != authorID && s.adapter.canAppSeeChannel(app, channelID)
	}
	s.disp.Emit(appsplatform.EventMessageCreated, payload, notSelfAndVisible)
	s.disp.Emit(appsplatform.EventAppMention, payload, func(app *appsplatform.App) bool {
		return notSelfAndVisible(app) && app.Mentions(text)
	})
}

// OnMemberJoined notifies apps that can see the channel that account joined.
func (s *AppsSink) OnMemberJoined(channelID, accountID string) {
	payload := map[string]any{"channel_id": channelID, "account_id": accountID}
	s.disp.Emit(appsplatform.EventMemberJoined, payload, func(app *appsplatform.App) bool {
		return app.AccountID() != accountID && s.adapter.canAppSeeChannel(app, channelID)
	})
}

// MaybeHandleSlash intercepts a registered slash command for an app targeting
// Talk and emits a slash_command event to the owning app, returning true so the
// caller does NOT store the body as a normal message. The payload carries both
// channel_id (Talk's historical key, kept for BOT-API compatibility) and the
// platform's generic target.
func (s *AppsSink) MaybeHandleSlash(channelID, userID, body string) bool {
	name, args, ok := appsplatform.ParseSlash(body)
	if !ok {
		return false
	}
	app, cmd, found := s.reg.ResolveSlashCommand(appsplatform.ProductTalk, name)
	if !found {
		return false
	}
	payload := map[string]any{
		"command":    cmd.Name,
		"text":       args,
		"channel_id": channelID,
		"target":     channelID,
		"user_id":    userID,
	}
	s.disp.Emit(appsplatform.EventSlashCommand, payload, func(a *appsplatform.App) bool {
		return a.ID == app.ID
	})
	return true
}
