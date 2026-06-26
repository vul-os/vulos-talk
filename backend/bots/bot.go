// Package bots implements the Vulos Talk bot/app framework: the bot model,
// token/secret minting, outbound-event signing, the registry "seam", a
// standalone (in-repo) registry backed by SQLite, and the event dispatcher.
//
// Registry seam
// -------------
// Like backend/seam, the Registry is an INTERFACE with a STANDALONE DEFAULT in
// this package (store_standalone.go). A Vulos Cloud control plane / developer
// console would implement the same Registry in a SEPARATE package (e.g.
// backend/integration/cloud) that the core NEVER imports — only the composition
// root (main.go) wires it, and only when explicitly selected. Removing the
// cloud package therefore never breaks the core build.
package bots

import (
	"strings"
	"time"
)

// Scope strings gate what a bot may do via the REST API. A bot with NO scopes
// can only call auth.test.
const (
	ScopeChatWrite      = "chat:write"      // POST messages / reactions authored by the bot
	ScopeHistoryRead    = "history:read"    // read channel message history
	ScopeChannelsRead   = "channels:read"   // list channels the bot can see
	ScopeMembersRead    = "members:read"    // list channel members
	ScopeReactionsWrite = "reactions:write" // add/remove reactions
)

// ValidScopes is the closed set of scope strings the framework understands.
// Unknown scopes are rejected at create/update time so a typo never silently
// grants (or appears to grant) access.
var ValidScopes = map[string]bool{
	ScopeChatWrite:      true,
	ScopeHistoryRead:    true,
	ScopeChannelsRead:   true,
	ScopeMembersRead:    true,
	ScopeReactionsWrite: true,
}

// SlashCommand is a slash command a bot registers. Name is stored WITHOUT the
// leading slash (e.g. "deploy", not "/deploy").
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Bot is a registered bot/app.
//
// Secrets:
//   - TokenHash is the sha256 hex of the bot token (Bearer secret). The
//     plaintext token is shown ONCE at create/rotate time and never stored.
//   - SigningSecret is stored AS-IS (not hashed): Talk must reproduce it to
//     sign every outbound event, so it cannot be a one-way hash. Treat the bot
//     row as sensitive at rest accordingly.
type Bot struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	OwnerID           string         `json:"owner_id"`            // creator account id
	OrgID             string         `json:"org_id"`              // empty in OSS / standalone
	Scopes            []string       `json:"scopes"`              // see Scope* constants
	EventURL          string         `json:"event_url"`           // outbound webhook (optional)
	IncomingWebhookID string         `json:"incoming_webhook_id"` // secret id in the incoming-webhook URL
	SlashCommands     []SlashCommand `json:"slash_commands"`
	DefaultChannel    string         `json:"default_channel"` // incoming-webhook fallback channel
	CreatedAt         time.Time      `json:"created_at"`

	// Secrets — never serialized in API responses (see BotSummary).
	TokenHash     string `json:"-"`
	SigningSecret string `json:"-"`
}

// AccountID is the synthetic author/membership id a bot posts and is addressed
// under: "bot:<id>".
func (b *Bot) AccountID() string { return BotAccountID(b.ID) }

// BotAccountID returns the synthetic account id for a bot id.
func BotAccountID(id string) string { return "bot:" + id }

// HasScope reports whether the bot was granted scope.
func (b *Bot) HasScope(scope string) bool {
	for _, s := range b.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Mentions reports whether body addresses this bot, either by its @name token
// or by its canonical <@bot:<id>> mention form (case-insensitive on the name).
func (b *Bot) Mentions(body string) bool {
	if b.Name != "" {
		needle := "@" + strings.ToLower(b.Name)
		if strings.Contains(strings.ToLower(body), needle) {
			return true
		}
	}
	return strings.Contains(body, "<@"+b.AccountID()+">")
}

// Summary is the secret-free public view of a bot returned by the admin API.
type Summary struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Scopes             []string       `json:"scopes"`
	EventURL           string         `json:"event_url"`
	SlashCommands      []SlashCommand `json:"slash_commands"`
	OwnerID            string         `json:"owner_id"`
	IncomingWebhookID  string         `json:"incoming_webhook_id"`
	IncomingWebhookURL string         `json:"incoming_webhook_url"`
	DefaultChannel     string         `json:"default_channel,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
}

// IncomingWebhookPath is the relative URL (path) clients POST to for an
// incoming webhook id.
func IncomingWebhookPath(webhookID string) string {
	return "/api/bot/hooks/" + webhookID
}

// ToSummary builds the secret-free Summary for a bot.
func (b *Bot) ToSummary() Summary {
	scopes := b.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	cmds := b.SlashCommands
	if cmds == nil {
		cmds = []SlashCommand{}
	}
	return Summary{
		ID:                 b.ID,
		Name:               b.Name,
		Scopes:             scopes,
		EventURL:           b.EventURL,
		SlashCommands:      cmds,
		OwnerID:            b.OwnerID,
		IncomingWebhookID:  b.IncomingWebhookID,
		IncomingWebhookURL: IncomingWebhookPath(b.IncomingWebhookID),
		DefaultChannel:     b.DefaultChannel,
		CreatedAt:          b.CreatedAt,
	}
}

// NormalizeScopes trims, lowercases, de-dupes and validates a scope list,
// returning the cleaned list or an error naming the first unknown scope.
func NormalizeScopes(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool)
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if !ValidScopes[s] {
			return nil, &ScopeError{Scope: s}
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// NormalizeSlashCommands trims command names (stripping a leading slash) and
// drops entries with empty names.
func NormalizeSlashCommands(in []SlashCommand) []SlashCommand {
	out := make([]SlashCommand, 0, len(in))
	seen := make(map[string]bool)
	for _, c := range in {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Name), "/")))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, SlashCommand{Name: name, Description: strings.TrimSpace(c.Description)})
	}
	return out
}

// ScopeError reports an unknown scope string.
type ScopeError struct{ Scope string }

func (e *ScopeError) Error() string { return "unknown scope: " + e.Scope }
