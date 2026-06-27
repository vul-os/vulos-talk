package bots

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// StandaloneRegistry is the in-repo default Registry. It keeps fast in-memory
// indexes (id, token-hash, incoming-webhook id, slash command) as the source of
// truth for lookups, and WRITES THROUGH to a pure-Go modernc SQLite database for
// durability. On open it rebuilds the indexes from the DB. Mirrors the storage
// style of backend/spaces/sqlite.go.
//
// With a nil db (NewMemoryRegistry) it is purely in-memory — used by tests and
// as a fallback when the durable DB cannot be opened.
type StandaloneRegistry struct {
	mu  sync.RWMutex
	db  *sql.DB // nil = in-memory only
	all map[string]*Bot
}

var _ Registry = (*StandaloneRegistry)(nil)

// NewMemoryRegistry builds an in-memory-only registry (no persistence).
func NewMemoryRegistry() *StandaloneRegistry {
	return &StandaloneRegistry{all: make(map[string]*Bot)}
}

// NewStandaloneRegistry opens (or creates) the SQLite database at dsn, ensures
// the schema, and loads existing bots into memory. Use a file path for
// durability or ":memory:" for an ephemeral DB.
func NewStandaloneRegistry(dsn string) (*StandaloneRegistry, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("bots: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	r := &StandaloneRegistry{db: db, all: make(map[string]*Bot)}
	if err := r.init(); err != nil {
		db.Close()
		return nil, err
	}
	if err := r.load(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the underlying database handle.
func (r *StandaloneRegistry) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *StandaloneRegistry) init() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS bots (
			id                  TEXT PRIMARY KEY,
			name                TEXT NOT NULL DEFAULT '',
			owner_id            TEXT NOT NULL DEFAULT '',
			org_id              TEXT NOT NULL DEFAULT '',
			scopes_json         TEXT NOT NULL DEFAULT '[]',
			event_url           TEXT NOT NULL DEFAULT '',
			incoming_webhook_id TEXT NOT NULL DEFAULT '',
			slash_json          TEXT NOT NULL DEFAULT '[]',
			default_channel     TEXT NOT NULL DEFAULT '',
			token_hash          TEXT NOT NULL DEFAULT '',
			signing_secret      TEXT NOT NULL DEFAULT '',
			created_at          INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_bots_token_hash ON bots(token_hash);
		CREATE INDEX IF NOT EXISTS idx_bots_webhook ON bots(incoming_webhook_id);
		CREATE INDEX IF NOT EXISTS idx_bots_owner ON bots(owner_id);
	`)
	if err != nil {
		return fmt.Errorf("bots: init schema: %w", err)
	}
	return nil
}

func (r *StandaloneRegistry) load() error {
	rows, err := r.db.Query(`SELECT id, name, owner_id, org_id, scopes_json, event_url,
		incoming_webhook_id, slash_json, default_channel, token_hash, signing_secret, created_at FROM bots`)
	if err != nil {
		return fmt.Errorf("bots: load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		b := &Bot{}
		var scopesJSON, slashJSON string
		var created int64
		if err := rows.Scan(&b.ID, &b.Name, &b.OwnerID, &b.OrgID, &scopesJSON, &b.EventURL,
			&b.IncomingWebhookID, &slashJSON, &b.DefaultChannel, &b.TokenHash, &b.SigningSecret, &created); err != nil {
			return fmt.Errorf("bots: scan: %w", err)
		}
		_ = json.Unmarshal([]byte(scopesJSON), &b.Scopes)
		_ = json.Unmarshal([]byte(slashJSON), &b.SlashCommands)
		b.CreatedAt = time.Unix(0, created)
		r.all[b.ID] = b
	}
	return rows.Err()
}

// persist writes a bot row (insert-or-replace). No-op when db is nil.
func (r *StandaloneRegistry) persist(b *Bot) error {
	if r.db == nil {
		return nil
	}
	scopesJSON, _ := json.Marshal(b.Scopes)
	slashJSON, _ := json.Marshal(b.SlashCommands)
	_, err := r.db.Exec(
		`INSERT INTO bots (id, name, owner_id, org_id, scopes_json, event_url,
			incoming_webhook_id, slash_json, default_channel, token_hash, signing_secret, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, owner_id=excluded.owner_id,
			org_id=excluded.org_id, scopes_json=excluded.scopes_json, event_url=excluded.event_url,
			incoming_webhook_id=excluded.incoming_webhook_id, slash_json=excluded.slash_json,
			default_channel=excluded.default_channel, token_hash=excluded.token_hash,
			signing_secret=excluded.signing_secret`,
		b.ID, b.Name, b.OwnerID, b.OrgID, string(scopesJSON), b.EventURL,
		b.IncomingWebhookID, string(slashJSON), b.DefaultChannel, b.TokenHash, b.SigningSecret,
		b.CreatedAt.UnixNano())
	return err
}

// clone returns a deep-ish copy so callers can't mutate registry-internal state
// through the returned pointer.
func clone(b *Bot) *Bot {
	if b == nil {
		return nil
	}
	cp := *b
	cp.Scopes = append([]string(nil), b.Scopes...)
	cp.SlashCommands = append([]SlashCommand(nil), b.SlashCommands...)
	return &cp
}

// Create implements Registry.
func (r *StandaloneRegistry) Create(p CreateParams) (*Created, error) {
	scopes, err := NormalizeScopes(p.Scopes)
	if err != nil {
		return nil, err
	}
	// SSRF guard (defense-in-depth; handlers also validate before reaching here).
	if err := ValidateEventURL(p.EventURL); err != nil {
		return nil, err
	}
	token := GenerateToken()
	secret := GenerateSecret()
	b := &Bot{
		ID:                GenerateBotID(),
		Name:              strings.TrimSpace(p.Name),
		OwnerID:           p.OwnerID,
		OrgID:             p.OrgID,
		Scopes:            scopes,
		EventURL:          strings.TrimSpace(p.EventURL),
		IncomingWebhookID: GenerateWebhookID(),
		SlashCommands:     NormalizeSlashCommands(p.SlashCommands),
		DefaultChannel:    strings.TrimSpace(p.DefaultChannel),
		CreatedAt:         time.Now(),
		TokenHash:         HashToken(token),
		SigningSecret:     secret,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.persist(b); err != nil {
		return nil, err
	}
	r.all[b.ID] = b
	return &Created{Bot: clone(b), Token: token, SigningSecret: secret}, nil
}

// Get implements Registry.
func (r *StandaloneRegistry) Get(id string) (*Bot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.all[id]; ok {
		return clone(b), nil
	}
	return nil, ErrNotFound
}

// GetByTokenHash implements Registry.
func (r *StandaloneRegistry) GetByTokenHash(tokenHash string) (*Bot, error) {
	if tokenHash == "" {
		return nil, ErrNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.all {
		if b.TokenHash == tokenHash {
			return clone(b), nil
		}
	}
	return nil, ErrNotFound
}

// GetByIncomingWebhookID implements Registry.
func (r *StandaloneRegistry) GetByIncomingWebhookID(webhookID string) (*Bot, error) {
	if webhookID == "" {
		return nil, ErrNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.all {
		// The incoming-webhook id IS the secret (the unauthenticated hook endpoint
		// has no other credential), so compare in constant time to avoid leaking
		// it via timing.
		if b.IncomingWebhookID != "" &&
			subtle.ConstantTimeCompare([]byte(b.IncomingWebhookID), []byte(webhookID)) == 1 {
			return clone(b), nil
		}
	}
	return nil, ErrNotFound
}

// List implements Registry.
func (r *StandaloneRegistry) List(owner string, isAdmin bool) ([]*Bot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Bot, 0, len(r.all))
	for _, b := range r.all {
		if isAdmin || b.OwnerID == owner {
			out = append(out, clone(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Update implements Registry.
func (r *StandaloneRegistry) Update(id string, p UpdateParams) (*Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.all[id]
	if !ok {
		return nil, ErrNotFound
	}
	updated := clone(b)
	if p.Name != nil {
		updated.Name = strings.TrimSpace(*p.Name)
	}
	if p.Scopes != nil {
		scopes, err := NormalizeScopes(*p.Scopes)
		if err != nil {
			return nil, err
		}
		updated.Scopes = scopes
	}
	if p.EventURL != nil {
		// SSRF guard (defense-in-depth; handlers also validate before reaching here).
		if err := ValidateEventURL(*p.EventURL); err != nil {
			return nil, err
		}
		updated.EventURL = strings.TrimSpace(*p.EventURL)
	}
	if p.SlashCommands != nil {
		updated.SlashCommands = NormalizeSlashCommands(*p.SlashCommands)
	}
	if p.DefaultChannel != nil {
		updated.DefaultChannel = strings.TrimSpace(*p.DefaultChannel)
	}
	if err := r.persist(updated); err != nil {
		return nil, err
	}
	r.all[id] = updated
	return clone(updated), nil
}

// Delete implements Registry.
func (r *StandaloneRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.all[id]; !ok {
		return ErrNotFound
	}
	if r.db != nil {
		if _, err := r.db.Exec(`DELETE FROM bots WHERE id = ?`, id); err != nil {
			return err
		}
	}
	delete(r.all, id)
	return nil
}

// RotateToken implements Registry.
func (r *StandaloneRegistry) RotateToken(id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.all[id]
	if !ok {
		return "", ErrNotFound
	}
	token := GenerateToken()
	updated := clone(b)
	updated.TokenHash = HashToken(token)
	if err := r.persist(updated); err != nil {
		return "", err
	}
	r.all[id] = updated
	return token, nil
}

// RotateSecret implements Registry.
func (r *StandaloneRegistry) RotateSecret(id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.all[id]
	if !ok {
		return "", ErrNotFound
	}
	secret := GenerateSecret()
	updated := clone(b)
	updated.SigningSecret = secret
	if err := r.persist(updated); err != nil {
		return "", err
	}
	r.all[id] = updated
	return secret, nil
}

// ResolveSlashCommand implements Registry.
func (r *StandaloneRegistry) ResolveSlashCommand(name string) (*Bot, *SlashCommand, bool) {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "/")))
	if name == "" {
		return nil, nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.all {
		for i := range b.SlashCommands {
			if b.SlashCommands[i].Name == name {
				cmd := b.SlashCommands[i]
				return clone(b), &cmd, true
			}
		}
	}
	return nil, nil, false
}

// AllSlashCommands implements Registry.
func (r *StandaloneRegistry) AllSlashCommands() []RegisteredCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegisteredCommand, 0)
	for _, b := range r.all {
		for _, cmd := range b.SlashCommands {
			out = append(out, RegisteredCommand{Name: cmd.Name, Description: cmd.Description, BotID: b.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
