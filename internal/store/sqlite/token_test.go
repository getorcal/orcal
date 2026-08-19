package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/auth"
)

func newTokenRepo(t *testing.T) (auth.Repo, context.Context) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store.Tokens(), context.Background()
}

func sampleToken(id, name string) *auth.Token {
	return &auth.Token{
		ID:        id,
		Name:      name,
		Hash:      auth.HashToken(id),
		Prefix:    "orcal_abcdef",
		Scopes:    auth.Scopes{auth.ScopeExec},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestTokenRoundTrip(t *testing.T) {
	repo, ctx := newTokenRepo(t)
	want := sampleToken("id-1", "ci")
	expires := want.CreatedAt.Add(time.Hour)
	want.ExpiresAt = &expires

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.GetByHash(ctx, want.Hash)
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.Prefix != want.Prefix {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != auth.ScopeExec {
		t.Fatalf("scopes did not survive: %v", got.Scopes)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("expires_at did not survive: %v", got.ExpiresAt)
	}
	if got.LastUsedAt != nil || got.RevokedAt != nil {
		t.Fatal("absent timestamps must decode as nil, not zero")
	}
}

func TestTokenNameIsUniqueOnlyAmongLiveTokens(t *testing.T) {
	repo, ctx := newTokenRepo(t)
	first := sampleToken("id-1", "ci")
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}

	clash := sampleToken("id-2", "ci")
	if err := repo.Create(ctx, clash); !errors.Is(err, auth.ErrNameTaken) {
		t.Fatalf("a live duplicate name must be refused, got %v", err)
	}

	revoked := time.Now().UTC()
	first.RevokedAt = &revoked
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := repo.Create(ctx, sampleToken("id-3", "ci")); err != nil {
		t.Fatalf("the name must be reusable after revocation: %v", err)
	}
}

func TestGetByNameIgnoresRevoked(t *testing.T) {
	repo, ctx := newTokenRepo(t)
	tok := sampleToken("id-1", "ci")
	revoked := time.Now().UTC()
	tok.RevokedAt = &revoked
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.GetByName(ctx, "ci"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected not found for a revoked name, got %v", err)
	}
}

func TestMissingTokenIsNotFound(t *testing.T) {
	repo, ctx := newTokenRepo(t)
	if _, err := repo.GetByHash(ctx, "nope"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.Update(ctx, sampleToken("ghost", "ghost")); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("updating a missing row must report ErrNotFound, got %v", err)
	}
}

func TestTouchLastUsed(t *testing.T) {
	repo, ctx := newTokenRepo(t)
	tok := sampleToken("id-1", "ci")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.TouchLastUsed(ctx, "id-1", at); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := repo.Get(ctx, "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) {
		t.Fatalf("last_used_at not persisted: %v", got.LastUsedAt)
	}
}
