package bots

import "errors"

// ErrNotFound is returned by Registry lookups/mutations when no bot matches.
var ErrNotFound = errors.New("bots: bot not found")

// CreateParams carries the caller-supplied fields for creating a bot. The
// registry assigns the id, secrets, and incoming-webhook id.
type CreateParams struct {
	Name           string
	OwnerID        string
	OrgID          string
	Scopes         []string
	EventURL       string
	SlashCommands  []SlashCommand
	DefaultChannel string
}

// UpdateParams carries the mutable fields for an update. Nil pointers mean
// "leave unchanged"; a non-nil pointer (even to an empty value) replaces the
// field.
type UpdateParams struct {
	Name           *string
	Scopes         *[]string
	EventURL       *string
	SlashCommands  *[]SlashCommand
	DefaultChannel *string
}

// Created bundles a freshly-created bot with its one-time plaintext secrets.
type Created struct {
	Bot           *Bot
	Token         string // plaintext bot token — shown once
	SigningSecret string // plaintext signing secret — shown once
}

// Registry is THE SEAM for bot storage and lookup.
//
// The STANDALONE DEFAULT (StandaloneRegistry, store_standalone.go) lives in this
// package and is backed by SQLite with an in-memory fallback. A Vulos Cloud
// developer console / control plane would implement this SAME interface in a
// separate package the core never imports; main.go decides which to wire.
type Registry interface {
	// Create persists a new bot, returning it alongside its one-time plaintext
	// token and signing secret.
	Create(p CreateParams) (*Created, error)

	// Get returns the bot by id, or ErrNotFound.
	Get(id string) (*Bot, error)

	// GetByTokenHash returns the bot whose token hash matches, or ErrNotFound.
	// Used by BotAuth — callers pass HashToken(plaintext).
	GetByTokenHash(tokenHash string) (*Bot, error)

	// GetByIncomingWebhookID returns the bot owning an incoming-webhook id.
	GetByIncomingWebhookID(webhookID string) (*Bot, error)

	// List returns bots visible to owner. When isAdmin is true ALL bots are
	// returned regardless of owner (admins manage everything).
	List(owner string, isAdmin bool) ([]*Bot, error)

	// Update mutates the named fields of a bot and returns the updated bot.
	Update(id string, p UpdateParams) (*Bot, error)

	// Delete removes a bot. Deleting an unknown id returns ErrNotFound.
	Delete(id string) error

	// RotateToken mints a new bot token, stores its hash, and returns the new
	// plaintext token (shown once).
	RotateToken(id string) (string, error)

	// RotateSecret mints a new signing secret and returns its plaintext (shown
	// once). It is stored as-is so outbound events can be signed.
	RotateSecret(id string) (string, error)

	// ResolveSlashCommand finds the bot + command that owns a slash command name
	// (without the leading slash). ok is false when no bot registered it.
	ResolveSlashCommand(name string) (*Bot, *SlashCommand, bool)

	// AllSlashCommands returns every registered slash command (for composer
	// autocomplete), annotated with its owning bot id.
	AllSlashCommands() []RegisteredCommand
}

// RegisteredCommand is a slash command annotated with its owning bot id, for
// the composer autocomplete surface.
type RegisteredCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	BotID       string `json:"bot_id"`
}
