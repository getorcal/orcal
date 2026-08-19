package auth

import (
	"context"
	"testing"
	"time"
)

type memSettings struct {
	values map[string]string
}

func newMemSettings() *memSettings { return &memSettings{values: map[string]string{}} }

func (m *memSettings) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memSettings) Set(_ context.Context, key, value string) error {
	m.values[key] = value
	return nil
}

func (m *memSettings) Delete(_ context.Context, key string) error {
	delete(m.values, key)
	return nil
}

func TestBootstrapGeneratesOnAnEmptyDatabase(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	plaintext, generated, err := svc.Bootstrap(ctx, newMemSettings(), "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !generated || plaintext == "" {
		t.Fatal("an empty database must generate and return a token")
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("expected exactly one token, got %d", len(repo.tokens))
	}
	tok := repo.tokens[0]
	if tok.Name != "bootstrap" {
		t.Fatalf("expected the token to be named bootstrap, got %q", tok.Name)
	}
	if !tok.Scopes.Has(ScopeSandboxesWrite) || len(tok.Scopes) != 1 || tok.Scopes[0] != ScopeAll {
		t.Fatalf("the bootstrap token must hold exactly the wildcard, got %v", tok.Scopes)
	}
	if tok.Prefix != PrefixOf(plaintext) {
		t.Fatal("a generated bootstrap token must record its prefix")
	}
}

func TestBootstrapIsIdempotent(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()
	settings := newMemSettings()

	if _, _, err := svc.Bootstrap(ctx, settings, ""); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	plaintext, generated, err := svc.Bootstrap(ctx, settings, "")
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if generated || plaintext != "" {
		t.Fatal("a second boot must not generate another token")
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("expected one token after two boots, got %d", len(repo.tokens))
	}
}

func TestBootstrapMigratesTheSettingsKey(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()
	settings := newMemSettings()
	legacyHash := HashToken("legacy-plaintext")
	settings.values[SettingKey] = legacyHash

	plaintext, generated, err := svc.Bootstrap(ctx, settings, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if generated || plaintext != "" {
		t.Fatal("migrating an existing token must not generate a new one")
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("expected exactly one token, got %d", len(repo.tokens))
	}
	tok := repo.tokens[0]
	if tok.Hash != legacyHash {
		t.Fatal("the migrated token must keep the operator's existing hash")
	}
	if tok.Prefix != "" {
		t.Fatalf("a migrated token has no recoverable prefix, got %q", tok.Prefix)
	}
	if _, found, _ := settings.Get(ctx, SettingKey); found {
		t.Fatal("the settings row must be deleted so there is one source of truth")
	}

	if _, err := svc.Authenticate(ctx, "legacy-plaintext"); err != nil {
		t.Fatalf("the operator's existing token must still work: %v", err)
	}
}

func TestBootstrapPinsTheConfiguredToken(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Bootstrap(ctx, newMemSettings(), "orcal_configured"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("expected one token, got %d", len(repo.tokens))
	}
	if repo.tokens[0].Hash != HashToken("orcal_configured") {
		t.Fatal("ORCAL_TOKEN must set the bootstrap hash")
	}
	if repo.tokens[0].Prefix != PrefixOf("orcal_configured") {
		t.Fatal("a configured token has a recoverable prefix")
	}
	if _, err := svc.Authenticate(ctx, "orcal_configured"); err != nil {
		t.Fatalf("the configured token must authenticate: %v", err)
	}
}

func TestBootstrapConfiguredTokenReplacesAnExistingBootstrap(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()
	settings := newMemSettings()

	if _, _, err := svc.Bootstrap(ctx, settings, ""); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if _, _, err := svc.Bootstrap(ctx, settings, "orcal_rescue"); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("pinning must update in place, not append: got %d tokens", len(repo.tokens))
	}
	if _, err := svc.Authenticate(ctx, "orcal_rescue"); err != nil {
		t.Fatalf("the rescue token must authenticate: %v", err)
	}
}

func TestBootstrapDoesNotResurrectARevokedBootstrapName(t *testing.T) {
	svc, repo, now := newTestService()
	ctx := context.Background()
	settings := newMemSettings()

	if _, _, err := svc.Bootstrap(ctx, settings, ""); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, _, err := svc.Create(ctx, CreateOptions{Name: "ops", Scopes: Scopes{ScopeAll}}, Scopes{ScopeAll}); err != nil {
		t.Fatalf("create ops: %v", err)
	}
	revoked := *now
	repo.tokens[0].RevokedAt = &revoked

	plaintext, generated, err := svc.Bootstrap(ctx, settings, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if generated || plaintext != "" {
		t.Fatal("another live token exists, so nothing must be generated")
	}
}

func TestBootstrapGeneratesWhenEveryTokenIsExpired(t *testing.T) {
	svc, repo, now := newTestService()
	ctx := context.Background()
	settings := newMemSettings()

	if _, _, err := svc.Create(ctx, CreateOptions{Name: "old", Scopes: Scopes{ScopeExec}, ExpiresIn: time.Hour}, Scopes{ScopeAll}); err != nil {
		t.Fatalf("create: %v", err)
	}
	later := now.Add(2 * time.Hour)
	svc.now = func() time.Time { return later }

	_, generated, err := svc.Bootstrap(ctx, settings, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !generated {
		t.Fatal("a daemon whose only token has expired is unreachable and must mint a new one")
	}
	if len(repo.tokens) != 2 {
		t.Fatalf("expected the expired token to be kept alongside the new one, got %d", len(repo.tokens))
	}
}
