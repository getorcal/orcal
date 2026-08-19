package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memRepo struct {
	tokens  []*Token
	touched int
}

func (m *memRepo) Create(_ context.Context, t *Token) error {
	for _, existing := range m.tokens {
		if existing.Name == t.Name && existing.RevokedAt == nil {
			return ErrNameTaken
		}
	}
	clone := *t
	m.tokens = append(m.tokens, &clone)
	return nil
}

func (m *memRepo) Get(_ context.Context, id string) (*Token, error) {
	for _, t := range m.tokens {
		if t.ID == id {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memRepo) GetByHash(_ context.Context, hash string) (*Token, error) {
	for _, t := range m.tokens {
		if t.Hash == hash {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memRepo) GetByName(_ context.Context, name string) (*Token, error) {
	for _, t := range m.tokens {
		if t.Name == name && t.RevokedAt == nil {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memRepo) List(_ context.Context) ([]*Token, error) {
	out := make([]*Token, 0, len(m.tokens))
	for _, t := range m.tokens {
		clone := *t
		out = append(out, &clone)
	}
	return out, nil
}

func (m *memRepo) Update(_ context.Context, t *Token) error {
	for i, existing := range m.tokens {
		if existing.ID == t.ID {
			clone := *t
			m.tokens[i] = &clone
			return nil
		}
	}
	return ErrNotFound
}

func (m *memRepo) TouchLastUsed(_ context.Context, id string, at time.Time) error {
	m.touched++
	for _, t := range m.tokens {
		if t.ID == id {
			stamp := at
			t.LastUsedAt = &stamp
			return nil
		}
	}
	return ErrNotFound
}

func newTestService() (*Service, *memRepo, *time.Time) {
	repo := &memRepo{}
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	counter := 0
	svc.newID = func() string {
		counter++
		return "token-" + string(rune('0'+counter))
	}
	return svc, repo, &now
}

func TestCreateReturnsPlaintextOnce(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()

	tok, plaintext, err := svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeExec}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if plaintext == "" {
		t.Fatal("Create must return the plaintext")
	}
	if tok.Hash != HashToken(plaintext) {
		t.Fatal("the stored hash must be the hash of the returned plaintext")
	}
	if tok.Prefix != PrefixOf(plaintext) {
		t.Fatal("the stored prefix must come from the plaintext")
	}

	listed, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Hash == plaintext {
		t.Fatal("the plaintext must never be recoverable from storage")
	}
}

func TestCreateRefusesEscalation(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()

	_, _, err := svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeSandboxesWrite}}, Scopes{ScopeAdmin})
	if !errors.Is(err, ErrScopeEscalation) {
		t.Fatalf("admin alone must not grant sandboxes:write, got %v", err)
	}

	_, _, err = svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeAdmin}}, Scopes{ScopeAdmin})
	if err != nil {
		t.Fatalf("granting a held scope must succeed: %v", err)
	}
}

func TestCreateValidates(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Create(ctx, CreateOptions{Name: "ci"}, Scopes{ScopeAll}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("an empty scope set must be refused, got %v", err)
	}
	if _, _, err := svc.Create(ctx, CreateOptions{Name: "BAD", Scopes: Scopes{ScopeExec}}, Scopes{ScopeAll}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("an invalid name must be refused, got %v", err)
	}
}

func TestCreateSetsExpiry(t *testing.T) {
	svc, _, now := newTestService()
	tok, _, err := svc.Create(context.Background(),
		CreateOptions{Name: "ci", Scopes: Scopes{ScopeExec}, ExpiresIn: 2 * time.Hour}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("expected expiry at now+2h, got %v", tok.ExpiresAt)
	}
}

func TestAuthenticate(t *testing.T) {
	svc, repo, now := newTestService()
	ctx := context.Background()
	_, plaintext, err := svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeExec}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.Authenticate(ctx, plaintext)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.Name != "ci" {
		t.Fatalf("wrong token resolved: %+v", got)
	}

	if _, err := svc.Authenticate(ctx, "orcal_wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an unknown token must be unauthorized, got %v", err)
	}
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an empty token must be unauthorized, got %v", err)
	}

	revoked := *now
	repo.tokens[0].RevokedAt = &revoked
	if _, err := svc.Authenticate(ctx, plaintext); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("a revoked token must be unauthorized, got %v", err)
	}

	repo.tokens[0].RevokedAt = nil
	past := now.Add(-time.Hour)
	repo.tokens[0].ExpiresAt = &past
	if _, err := svc.Authenticate(ctx, plaintext); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an expired token must be unauthorized, got %v", err)
	}
}

func TestAuthenticateThrottlesLastUsedWrites(t *testing.T) {
	svc, repo, now := newTestService()
	ctx := context.Background()
	_, plaintext, err := svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeExec}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for range 5 {
		if _, err := svc.Authenticate(ctx, plaintext); err != nil {
			t.Fatalf("authenticate: %v", err)
		}
	}
	if repo.touched != 1 {
		t.Fatalf("five authentications inside one minute must write last_used_at once, got %d", repo.touched)
	}

	later := now.Add(2 * time.Minute)
	svc.now = func() time.Time { return later }
	if _, err := svc.Authenticate(ctx, plaintext); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if repo.touched != 2 {
		t.Fatalf("an authentication after the interval must write again, got %d", repo.touched)
	}
}

func TestRevokeIsIdempotentAndGuardsTheLastAdmin(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()

	root, _, err := svc.Create(ctx, CreateOptions{Name: "root", Scopes: Scopes{ScopeAll}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := svc.Revoke(ctx, root.ID); !errors.Is(err, ErrLastAdminToken) {
		t.Fatalf("revoking the only admin-capable token must be refused, got %v", err)
	}

	second, _, err := svc.Create(ctx, CreateOptions{Name: "ops", Scopes: Scopes{ScopeAdmin}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create ops: %v", err)
	}
	if err := svc.Revoke(ctx, root.ID); err != nil {
		t.Fatalf("revoking with another admin present must succeed: %v", err)
	}
	if err := svc.Revoke(ctx, root.ID); err != nil {
		t.Fatalf("revoking an already-revoked token must be a no-op, got %v", err)
	}
	if err := svc.Revoke(ctx, second.ID); !errors.Is(err, ErrLastAdminToken) {
		t.Fatalf("the last remaining admin must be protected, got %v", err)
	}
	if err := svc.Revoke(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking an unknown id must report ErrNotFound, got %v", err)
	}
}

func TestRevokeAllowsNonAdminFreely(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, _, err := svc.Create(ctx, CreateOptions{Name: "root", Scopes: Scopes{ScopeAll}}, Scopes{ScopeAll}); err != nil {
		t.Fatalf("create root: %v", err)
	}
	ci, _, err := svc.Create(ctx, CreateOptions{Name: "ci", Scopes: Scopes{ScopeExec}}, Scopes{ScopeAll})
	if err != nil {
		t.Fatalf("create ci: %v", err)
	}
	if err := svc.Revoke(ctx, ci.ID); err != nil {
		t.Fatalf("revoking a non-admin token must succeed: %v", err)
	}
}
