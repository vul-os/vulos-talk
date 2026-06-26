package handlers

// BotSink decouples the spaces handlers from the bots package. The bots
// dispatcher implements it; the spaces handler calls it (when set) after a
// successful send / reply / join so bot events fan out, and consults
// MaybeHandleSlash on the send path to intercept slash commands.
//
// Declaring the interface HERE (consumer side) keeps backend/spaces and the
// spaces handlers free of any import of backend/bots — only main.go wires the
// concrete dispatcher in via SpacesHandler.SetBotSink.
type BotSink interface {
	// OnMessageCreated is called after a new top-level/thread message is stored.
	OnMessageCreated(channelID, msgID, authorID, text, threadParent string)
	// OnMemberJoined is called after an account joins a channel.
	OnMemberJoined(channelID, accountID string)
	// MaybeHandleSlash reports whether body was a registered slash command and
	// was dispatched as such (so it must NOT be stored as a normal message).
	MaybeHandleSlash(channelID, userID, body string) bool
}
