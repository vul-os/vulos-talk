// postgres.go — durable Persister for Vulos Spaces backed by Postgres (pgx/v5).
//
// Cloud consolidation: when the runtime is pointed at a shared Postgres
// database (DATABASE_URL / VULOS_DATABASE_URL, wired in handlers/spaces.go) the
// SpacesStore persists into a dedicated `talk` schema so a single Neon database
// can host every Vulos product side-by-side without table-name collisions. When
// no DSN is set the embedded SQLite Persister (sqlite.go) remains the default,
// keeping the self-host / open-core path unchanged.
//
// The schema mirrors sqlite.go one-for-one (same columns, same semantics):
// timestamps are stored as BIGINT UnixNano so the round-trip is byte-identical
// to the SQLite backend and the public Persister behavior is unchanged.
package spaces

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vulos-talk/backend/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresSchema is the dedicated Postgres schema all Talk tables live in so a
// single (Neon) database can be shared across Vulos products.
const PostgresSchema = "talk"

// PostgresPersister stores Spaces state in a Postgres database under the `talk`
// schema. It implements Persister and Searcher (Postgres full-text search).
type PostgresPersister struct {
	pool *pgxpool.Pool
}

// Compile-time guarantees that the Postgres backend satisfies the same contract
// as the SQLite backend (durable persistence + full-text search).
var (
	_ Persister = (*PostgresPersister)(nil)
	_ Searcher  = (*PostgresPersister)(nil)
)

// NewPostgresPersister opens a pool against dsn, ensures the `talk` schema and
// tables exist, and returns a ready persister. dsn may be a URL
// (postgres://…) or keyword DSN; pgx accepts both.
func NewPostgresPersister(dsn string) (*PostgresPersister, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("spaces: parse postgres dsn: %w", err)
	}
	// Pin search_path on every pooled connection so unqualified identifiers and
	// the to_tsvector/to_tsquery lookups resolve inside the talk schema. All DML
	// below is additionally schema-qualified, so this is belt-and-suspenders.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+PostgresSchema+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("spaces: connect postgres: %w", err)
	}
	p := &PostgresPersister{pool: pool}
	if err := p.init(); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

// Close releases the underlying connection pool.
func (p *PostgresPersister) Close() error {
	if p.pool != nil {
		p.pool.Close()
	}
	return nil
}

func (p *PostgresPersister) init() error {
	ctx := context.Background()
	// Multi-statement DDL with no bind args runs over the simple query protocol
	// (same pattern as backend/storage/postgres.go). All idempotent.
	_, err := p.pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS talk;
		CREATE TABLE IF NOT EXISTS talk.channels (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			type       TEXT NOT NULL DEFAULT 'public',
			created_by TEXT NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL DEFAULT 0,
			updated_at BIGINT NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS talk.memberships (
			id           TEXT NOT NULL,
			channel_id   TEXT NOT NULL,
			account_id   TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			joined_at    BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (channel_id, account_id)
		);
		CREATE TABLE IF NOT EXISTS talk.messages (
			id            TEXT NOT NULL,
			channel_id    TEXT NOT NULL,
			thread_parent TEXT NOT NULL DEFAULT '',
			author_id     TEXT NOT NULL DEFAULT '',
			body          TEXT NOT NULL DEFAULT '',
			state         TEXT NOT NULL DEFAULT 'active',
			seq_clock     TEXT NOT NULL DEFAULT '',
			created_at    BIGINT NOT NULL DEFAULT 0,
			updated_at    BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (channel_id, id)
		);
		CREATE TABLE IF NOT EXISTS talk.ops (
			channel_id TEXT NOT NULL,
			op         TEXT NOT NULL,
			seq_clock  TEXT NOT NULL,
			msg_json   TEXT NOT NULL,
			applied_at BIGINT NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_ops_channel_seq ON talk.ops(channel_id, seq_clock);
		CREATE INDEX IF NOT EXISTS idx_messages_channel ON talk.messages(channel_id);
		-- Postgres full-text index over message bodies (Searcher capability).
		CREATE INDEX IF NOT EXISTS idx_messages_fts ON talk.messages
			USING GIN (to_tsvector('simple', body));
		CREATE TABLE IF NOT EXISTS talk.read_state (
			account_id      TEXT NOT NULL,
			channel_id      TEXT NOT NULL,
			last_read_clock TEXT NOT NULL DEFAULT '',
			updated_at      BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (account_id, channel_id)
		);
		CREATE TABLE IF NOT EXISTS talk.user_status (
			user_id     TEXT PRIMARY KEY,
			status      TEXT NOT NULL DEFAULT 'online',
			custom_text TEXT NOT NULL DEFAULT '',
			until_unix  BIGINT NOT NULL DEFAULT 0,
			updated_at  BIGINT NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS talk.reactions (
			message_id TEXT NOT NULL,
			emoji      TEXT NOT NULL,
			user_id    TEXT NOT NULL,
			created_at BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (message_id, emoji, user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_reactions_msg ON talk.reactions(message_id);
		CREATE TABLE IF NOT EXISTS talk.pins (
			channel_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			author_id  TEXT NOT NULL DEFAULT '',
			body       TEXT NOT NULL DEFAULT '',
			pinned_by  TEXT NOT NULL DEFAULT '',
			pinned_at  BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (channel_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_pins_channel ON talk.pins(channel_id);
	`)
	if err != nil {
		return fmt.Errorf("spaces: init postgres schema: %w", err)
	}
	return nil
}

// ---- channels ----------------------------------------------------------------

func (p *PostgresPersister) SaveChannel(ch *models.Channel) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.channels (id, name, type, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type,
		   created_by=excluded.created_by, updated_at=excluded.updated_at`,
		ch.ID, ch.Name, string(ch.Type), ch.CreatedBy,
		ch.CreatedAt.UnixNano(), ch.UpdatedAt.UnixNano())
	return err
}

func (p *PostgresPersister) ListChannels() ([]*models.Channel, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT id, name, type, created_by, created_at, updated_at FROM talk.channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Channel
	for rows.Next() {
		var ch models.Channel
		var ctype string
		var created, updated int64
		if err := rows.Scan(&ch.ID, &ch.Name, &ctype, &ch.CreatedBy, &created, &updated); err != nil {
			return nil, err
		}
		ch.Type = models.ChannelType(ctype)
		ch.CreatedAt = time.Unix(0, created)
		ch.UpdatedAt = time.Unix(0, updated)
		out = append(out, &ch)
	}
	return out, rows.Err()
}

func (p *PostgresPersister) GetChannel(id string) (*models.Channel, error) {
	row := p.pool.QueryRow(context.Background(),
		`SELECT id, name, type, created_by, created_at, updated_at FROM talk.channels WHERE id = $1`, id)
	var ch models.Channel
	var ctype string
	var created, updated int64
	if err := row.Scan(&ch.ID, &ch.Name, &ctype, &ch.CreatedBy, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("channel not found: %s", id)
		}
		return nil, err
	}
	ch.Type = models.ChannelType(ctype)
	ch.CreatedAt = time.Unix(0, created)
	ch.UpdatedAt = time.Unix(0, updated)
	return &ch, nil
}

func (p *PostgresPersister) DeleteChannel(id string) error {
	_, err := p.pool.Exec(context.Background(), `DELETE FROM talk.channels WHERE id = $1`, id)
	return err
}

// ---- memberships -------------------------------------------------------------

func (p *PostgresPersister) SaveMembership(m *models.Membership) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.memberships (id, channel_id, account_id, display_name, joined_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT(channel_id, account_id) DO NOTHING`,
		m.ID, m.ChannelID, m.AccountID, m.DisplayName, m.JoinedAt.UnixNano())
	return err
}

func (p *PostgresPersister) ListMemberships(channelID string) ([]*models.Membership, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT id, channel_id, account_id, display_name, joined_at FROM talk.memberships WHERE channel_id = $1`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Membership
	for rows.Next() {
		var m models.Membership
		var joined int64
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.AccountID, &m.DisplayName, &joined); err != nil {
			return nil, err
		}
		m.JoinedAt = time.Unix(0, joined)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (p *PostgresPersister) DeleteMembership(channelID, accountID string) error {
	_, err := p.pool.Exec(context.Background(),
		`DELETE FROM talk.memberships WHERE channel_id = $1 AND account_id = $2`, channelID, accountID)
	return err
}

func (p *PostgresPersister) SetMembershipName(channelID, accountID, displayName string) error {
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE talk.memberships SET display_name = $1 WHERE channel_id = $2 AND account_id = $3`,
		displayName, channelID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}

// ---- messages ----------------------------------------------------------------

func (p *PostgresPersister) SaveMessage(msg *models.Message) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.messages (id, channel_id, thread_parent, author_id, body, state, seq_clock, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT(channel_id, id) DO UPDATE SET
		   thread_parent=excluded.thread_parent, author_id=excluded.author_id,
		   body=excluded.body, state=excluded.state, seq_clock=excluded.seq_clock,
		   updated_at=excluded.updated_at`,
		msg.ID, msg.ChannelID, msg.ThreadParent, msg.AuthorID, msg.Body,
		string(msg.State), msg.SeqClock, msg.CreatedAt.UnixNano(), msg.UpdatedAt.UnixNano())
	return err
}

func (p *PostgresPersister) scanMessages(rows pgx.Rows) ([]*models.Message, error) {
	defer rows.Close()
	var out []*models.Message
	for rows.Next() {
		var m models.Message
		var state string
		var created, updated int64
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.ThreadParent, &m.AuthorID, &m.Body, &state, &m.SeqClock, &created, &updated); err != nil {
			return nil, err
		}
		m.State = models.MessageState(state)
		m.CreatedAt = time.Unix(0, created)
		m.UpdatedAt = time.Unix(0, updated)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (p *PostgresPersister) ListMessages(channelID string) ([]*models.Message, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT id, channel_id, thread_parent, author_id, body, state, seq_clock, created_at, updated_at
		 FROM talk.messages WHERE channel_id = $1`, channelID)
	if err != nil {
		return nil, err
	}
	return p.scanMessages(rows)
}

func (p *PostgresPersister) GetMessage(channelID, id string) (*models.Message, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT id, channel_id, thread_parent, author_id, body, state, seq_clock, created_at, updated_at
		 FROM talk.messages WHERE channel_id = $1 AND id = $2`, channelID, id)
	if err != nil {
		return nil, err
	}
	msgs, err := p.scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("message not found: %s", id)
	}
	return msgs[0], nil
}

// SearchMessages runs a Postgres full-text MATCH over the channel's message
// bodies and returns matching ids most-recent first. Implements spaces.Searcher.
// Each token becomes a prefix term (`token:*`) AND-ed together, mirroring the
// SQLite FTS5 prefix semantics ("deploy" matches "deployment").
func (p *PostgresPersister) SearchMessages(channelID string, terms []string) ([]string, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	query := buildPGTSQuery(terms)
	if query == "" {
		return nil, nil
	}
	rows, err := p.pool.Query(context.Background(),
		`SELECT id FROM talk.messages
		 WHERE channel_id = $1 AND state <> $2
		   AND to_tsvector('simple', body) @@ to_tsquery('simple', $3)
		 ORDER BY seq_clock DESC`,
		channelID, string(models.MessageStateTombed), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// buildPGTSQuery turns plain word tokens into a safe to_tsquery expression:
// each token is reduced to [a-z0-9] (so it can never inject tsquery operators),
// suffixed with `:*` for prefix matching, and AND-ed together with `&`.
func buildPGTSQuery(terms []string) string {
	var parts []string
	for _, t := range terms {
		clean := sanitizePGToken(t)
		if clean == "" {
			continue
		}
		parts = append(parts, clean+":*")
	}
	return strings.Join(parts, " & ")
}

// sanitizePGToken keeps only ASCII letters/digits (lower-cased) so user input
// can never be parsed as to_tsquery syntax.
func sanitizePGToken(t string) string {
	var b strings.Builder
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	return b.String()
}

// ---- ops log -----------------------------------------------------------------

func (p *PostgresPersister) AppendOp(op *models.MessageOp) error {
	msgJSON, err := marshalMessage(&op.Msg)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(context.Background(),
		`INSERT INTO talk.ops (channel_id, op, seq_clock, msg_json, applied_at) VALUES ($1, $2, $3, $4, $5)`,
		op.ChannelID, string(op.Op), op.Msg.SeqClock, msgJSON, op.AppliedAt.UnixNano())
	return err
}

func (p *PostgresPersister) ListOps(channelID string, afterClock string) ([]*models.MessageOp, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT channel_id, op, msg_json, applied_at FROM talk.ops
		 WHERE channel_id = $1 AND seq_clock > $2 ORDER BY seq_clock ASC`,
		channelID, afterClock)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.MessageOp
	for rows.Next() {
		var op models.MessageOp
		var opType, msgJSON string
		var applied int64
		if err := rows.Scan(&op.ChannelID, &opType, &msgJSON, &applied); err != nil {
			return nil, err
		}
		op.Op = models.MessageOpType(opType)
		op.AppliedAt = time.Unix(0, applied)
		msg, err := unmarshalMessage(msgJSON)
		if err != nil {
			return nil, err
		}
		op.Msg = *msg
		out = append(out, &op)
	}
	return out, rows.Err()
}

// ---- read-state --------------------------------------------------------------

func (p *PostgresPersister) SaveReadState(rs *models.ReadState) error {
	ctx := context.Background()
	// LWW: only advance when the incoming clock is strictly newer.
	row := p.pool.QueryRow(ctx,
		`SELECT last_read_clock FROM talk.read_state WHERE account_id = $1 AND channel_id = $2`,
		rs.AccountID, rs.ChannelID)
	var existing string
	switch err := row.Scan(&existing); {
	case err == nil:
		if rs.LastReadClock <= existing {
			return nil
		}
	case errors.Is(err, pgx.ErrNoRows):
		// insert below
	default:
		return err
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO talk.read_state (account_id, channel_id, last_read_clock, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(account_id, channel_id) DO UPDATE SET
		   last_read_clock=excluded.last_read_clock, updated_at=excluded.updated_at`,
		rs.AccountID, rs.ChannelID, rs.LastReadClock, rs.UpdatedAt.UnixNano())
	return err
}

func (p *PostgresPersister) GetReadState(accountID, channelID string) (*models.ReadState, error) {
	row := p.pool.QueryRow(context.Background(),
		`SELECT last_read_clock, updated_at FROM talk.read_state WHERE account_id = $1 AND channel_id = $2`,
		accountID, channelID)
	rs := &models.ReadState{AccountID: accountID, ChannelID: channelID}
	var updated int64
	switch err := row.Scan(&rs.LastReadClock, &updated); {
	case err == nil:
		rs.UpdatedAt = time.Unix(0, updated)
		return rs, nil
	case errors.Is(err, pgx.ErrNoRows):
		return rs, nil
	default:
		return nil, err
	}
}

// ---- presence: user status --------------------------------------------------

func (p *PostgresPersister) SaveStatus(s *models.UserStatus) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.user_status (user_id, status, custom_text, until_unix, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT(user_id) DO UPDATE SET status=excluded.status,
		   custom_text=excluded.custom_text, until_unix=excluded.until_unix,
		   updated_at=excluded.updated_at`,
		s.UserID, s.Status, s.CustomText, s.UntilUnix, s.UpdatedAt.UnixNano())
	return err
}

func (p *PostgresPersister) ListStatuses() ([]*models.UserStatus, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT user_id, status, custom_text, until_unix, updated_at FROM talk.user_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.UserStatus
	for rows.Next() {
		var s models.UserStatus
		var updated int64
		if err := rows.Scan(&s.UserID, &s.Status, &s.CustomText, &s.UntilUnix, &updated); err != nil {
			return nil, err
		}
		s.UpdatedAt = time.Unix(0, updated)
		out = append(out, &s)
	}
	return out, rows.Err()
}

// ---- presence: reactions -----------------------------------------------------

func (p *PostgresPersister) SaveReaction(r *models.Reaction) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.reactions (message_id, emoji, user_id, created_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(message_id, emoji, user_id) DO NOTHING`,
		r.MessageID, r.Emoji, r.UserID, r.CreatedAt.UnixNano())
	return err
}

func (p *PostgresPersister) DeleteReaction(msgID, emoji, userID string) error {
	_, err := p.pool.Exec(context.Background(),
		`DELETE FROM talk.reactions WHERE message_id = $1 AND emoji = $2 AND user_id = $3`,
		msgID, emoji, userID)
	return err
}

func (p *PostgresPersister) ListReactions() ([]*models.Reaction, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT message_id, emoji, user_id, created_at FROM talk.reactions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Reaction
	for rows.Next() {
		var r models.Reaction
		var created int64
		if err := rows.Scan(&r.MessageID, &r.Emoji, &r.UserID, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(0, created)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ---- presence: pins ----------------------------------------------------------

func (p *PostgresPersister) SavePin(pin *models.PinnedMessage) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO talk.pins (channel_id, message_id, author_id, body, pinned_by, pinned_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT(channel_id, message_id) DO UPDATE SET author_id=excluded.author_id,
		   body=excluded.body, pinned_by=excluded.pinned_by, pinned_at=excluded.pinned_at`,
		pin.ChannelID, pin.MessageID, pin.AuthorID, pin.Body, pin.PinnedBy, pin.PinnedAt.UnixNano())
	return err
}

func (p *PostgresPersister) DeletePin(channelID, msgID string) error {
	_, err := p.pool.Exec(context.Background(),
		`DELETE FROM talk.pins WHERE channel_id = $1 AND message_id = $2`, channelID, msgID)
	return err
}

func (p *PostgresPersister) ListPins() ([]*models.PinnedMessage, error) {
	rows, err := p.pool.Query(context.Background(),
		`SELECT channel_id, message_id, author_id, body, pinned_by, pinned_at FROM talk.pins`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.PinnedMessage
	for rows.Next() {
		var pin models.PinnedMessage
		var pinnedAt int64
		if err := rows.Scan(&pin.ChannelID, &pin.MessageID, &pin.AuthorID, &pin.Body, &pin.PinnedBy, &pinnedAt); err != nil {
			return nil, err
		}
		pin.PinnedAt = time.Unix(0, pinnedAt)
		out = append(out, &pin)
	}
	return out, rows.Err()
}
