package bots

import (
	"testing"

	"vulos-talk/backend/models"
)

func TestCreateAndTokenHashLookup(t *testing.T) {
	r := NewMemoryRegistry()
	created, err := r.Create(CreateParams{Name: "deploybot", OwnerID: "alice", Scopes: []string{ScopeChatWrite}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Token == "" || created.SigningSecret == "" {
		t.Fatalf("expected plaintext token + secret on create")
	}
	if created.Bot.TokenHash != HashToken(created.Token) {
		t.Fatalf("stored hash does not match token hash")
	}

	// Lookup by hash succeeds; lookup by a wrong hash fails.
	got, err := r.GetByTokenHash(HashToken(created.Token))
	if err != nil || got.ID != created.Bot.ID {
		t.Fatalf("GetByTokenHash failed: %v", err)
	}
	if _, err := r.GetByTokenHash(HashToken("vbt_wrong")); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown token, got %v", err)
	}
}

func TestCreateRejectsUnknownScope(t *testing.T) {
	r := NewMemoryRegistry()
	_, err := r.Create(CreateParams{Name: "x", OwnerID: "a", Scopes: []string{"chat:write", "do:anything"}})
	var se *ScopeError
	if err == nil || !asScopeError(err, &se) {
		t.Fatalf("expected ScopeError, got %v", err)
	}
}

func asScopeError(err error, target **ScopeError) bool {
	if se, ok := err.(*ScopeError); ok {
		*target = se
		return true
	}
	return false
}

func TestListOwnerScoping(t *testing.T) {
	r := NewMemoryRegistry()
	_, _ = r.Create(CreateParams{Name: "a1", OwnerID: "alice"})
	_, _ = r.Create(CreateParams{Name: "b1", OwnerID: "bob"})

	alice, _ := r.List("alice", false)
	if len(alice) != 1 || alice[0].OwnerID != "alice" {
		t.Fatalf("alice should see only her bot, got %d", len(alice))
	}
	admin, _ := r.List("alice", true)
	if len(admin) != 2 {
		t.Fatalf("admin should see all bots, got %d", len(admin))
	}
}

func TestRotateTokenInvalidatesOld(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{Name: "x", OwnerID: "a"})
	oldHash := HashToken(created.Token)

	newToken, err := r.RotateToken(created.Bot.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := r.GetByTokenHash(oldHash); err != ErrNotFound {
		t.Fatalf("old token still valid after rotation")
	}
	if _, err := r.GetByTokenHash(HashToken(newToken)); err != nil {
		t.Fatalf("new token not valid: %v", err)
	}
}

func TestResolveSlashCommand(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{
		Name:          "ci",
		OwnerID:       "a",
		SlashCommands: []SlashCommand{{Name: "/deploy", Description: "ship it"}},
	})
	bot, cmd, ok := r.ResolveSlashCommand("deploy")
	if !ok || bot.ID != created.Bot.ID || cmd.Name != "deploy" {
		t.Fatalf("expected to resolve deploy, got ok=%v", ok)
	}
	if _, _, ok := r.ResolveSlashCommand("unknown"); ok {
		t.Fatalf("unknown command should not resolve")
	}
	all := r.AllSlashCommands()
	if len(all) != 1 || all[0].BotID != created.Bot.ID {
		t.Fatalf("AllSlashCommands wrong: %+v", all)
	}
}

func TestUpdateScopesAndDelete(t *testing.T) {
	r := NewMemoryRegistry()
	created, _ := r.Create(CreateParams{Name: "x", OwnerID: "a", Scopes: []string{ScopeChatWrite}})
	scopes := []string{ScopeChatWrite, ScopeHistoryRead}
	updated, err := r.Update(created.Bot.ID, UpdateParams{Scopes: &scopes})
	if err != nil || !updated.HasScope(ScopeHistoryRead) {
		t.Fatalf("update scopes failed: %v", err)
	}
	if err := r.Delete(created.Bot.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get(created.Bot.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete")
	}
}

func TestSQLiteRegistryPersistsAcrossReopen(t *testing.T) {
	dsn := t.TempDir() + "/bots.db"
	r, err := NewStandaloneRegistry(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	created, err := r.Create(CreateParams{Name: "persist", OwnerID: "alice", Scopes: []string{ScopeChatWrite}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tokenHash := HashToken(created.Token)
	r.Close()

	r2, err := NewStandaloneRegistry(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	got, err := r2.GetByTokenHash(tokenHash)
	if err != nil || got.Name != "persist" {
		t.Fatalf("bot did not survive reopen: %v", err)
	}
}

// dispatcher channel-visibility uses bots.Spaces; ensure the bot account id is
// the membership key used for private channels.
func TestBotAccountIDForMembership(t *testing.T) {
	b := &Bot{ID: "abc"}
	if b.AccountID() != "bot:abc" {
		t.Fatalf("unexpected bot account id %q", b.AccountID())
	}
	_ = models.ChannelTypePrivate // keep models import meaningful
}
