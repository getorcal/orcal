package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getorcal/orcal/internal/auth"
)

type memStore struct {
	values map[string]string
	getErr error
	setErr error
}

func newMemStore() *memStore { return &memStore{values: map[string]string{}} }

func (m *memStore) Get(ctx context.Context, key string) (string, bool, error) {
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memStore) Set(ctx context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.values[key] = value
	return nil
}

func TestGenerateTokenProducesDistinctHighEntropyTokens(t *testing.T) {
	seen := map[string]bool{}
	for i := range 100 {
		tok, err := auth.GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken() error = %v", err)
		}
		if len(tok) < 32 {
			t.Fatalf("token length = %d, want at least 32", len(tok))
		}
		if seen[tok] {
			t.Fatalf("GenerateToken() returned a duplicate on iteration %d", i)
		}
		seen[tok] = true
	}
}

func TestHashTokenIsStableAndNotThePlaintext(t *testing.T) {
	h1 := auth.HashToken("secret")
	h2 := auth.HashToken("secret")
	if h1 != h2 {
		t.Errorf("HashToken not stable: %q != %q", h1, h2)
	}
	if h1 == "secret" {
		t.Error("HashToken returned the plaintext")
	}
	if h1 == auth.HashToken("secret2") {
		t.Error("HashToken collided on different inputs")
	}
}

func TestVerifyAcceptsMatchAndRejectsMismatch(t *testing.T) {
	hash := auth.HashToken("secret")
	if !auth.Verify(hash, "secret") {
		t.Error("Verify(correct) = false, want true")
	}
	if auth.Verify(hash, "wrong") {
		t.Error("Verify(wrong) = true, want false")
	}
	if auth.Verify(hash, "") {
		t.Error("Verify(empty) = true, want false")
	}
}

func TestEnsureGeneratesAndPersistsOnFirstBoot(t *testing.T) {
	store := newMemStore()

	token, generated, err := auth.Ensure(context.Background(), store, "")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !generated {
		t.Error("generated = false on first boot, want true")
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	stored := store.values[auth.SettingKey]
	if stored != auth.HashToken(token) {
		t.Error("persisted value is not the hash of the returned token")
	}
}

func TestEnsureReusesPersistedHashOnSecondBoot(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	first, _, _ := auth.Ensure(ctx, store, "")

	second, generated, err := auth.Ensure(ctx, store, "")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if generated {
		t.Error("generated = true on second boot, want false")
	}
	if second != "" {
		t.Errorf("token = %q on second boot, want empty since the plaintext is not stored", second)
	}
	if store.values[auth.SettingKey] != auth.HashToken(first) {
		t.Error("persisted hash changed on second boot")
	}
}

func TestEnsurePrefersConfiguredToken(t *testing.T) {
	store := newMemStore()

	token, generated, err := auth.Ensure(context.Background(), store, "configured-token")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if generated {
		t.Error("generated = true with a configured token, want false")
	}
	if token != "configured-token" {
		t.Errorf("token = %q, want configured-token", token)
	}
	if store.values[auth.SettingKey] != auth.HashToken("configured-token") {
		t.Error("configured token hash was not persisted")
	}
}

func TestEnsurePropagatesTransientGetErrorWithoutMintingToken(t *testing.T) {
	store := newMemStore()
	store.getErr = errors.New("transient failure")

	token, generated, err := auth.Ensure(context.Background(), store, "")
	if err == nil {
		t.Fatal("Ensure() error = nil, want the transient error")
	}
	if generated {
		t.Error("generated = true on a transient Get error, want false")
	}
	if token != "" {
		t.Errorf("token = %q, want empty on a transient Get error", token)
	}
	if _, ok := store.values[auth.SettingKey]; ok {
		t.Error("Set was called despite a transient Get error")
	}
}

func TestEnsurePropagatesSetErrorOnGenerationPath(t *testing.T) {
	store := newMemStore()
	store.setErr = errors.New("set failed")

	token, generated, err := auth.Ensure(context.Background(), store, "")
	if err == nil {
		t.Fatal("Ensure() error = nil, want the Set error")
	}
	if generated {
		t.Error("generated = true despite Set failing, want false")
	}
	if token != "" {
		t.Errorf("token = %q, want empty since persistence failed", token)
	}
}
