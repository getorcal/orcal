package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryRepoCreateRejectsADuplicateHash(t *testing.T) {
	repo := NewMemoryRepo()
	ctx := context.Background()

	first := &Token{ID: "id-1", Name: "first", Hash: "same-hash"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	second := &Token{ID: "id-2", Name: "second", Hash: "same-hash"}
	if err := repo.Create(ctx, second); !errors.Is(err, ErrHashTaken) {
		t.Fatalf("Create() error = %v, want ErrHashTaken", err)
	}
}

func TestMemoryRepoCreateRejectsADuplicateHashEvenAfterRevocation(t *testing.T) {
	repo := NewMemoryRepo()
	ctx := context.Background()

	first := &Token{ID: "id-1", Name: "first", Hash: "same-hash"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	revoked := *first
	now := time.Now()
	revoked.RevokedAt = &now
	if err := repo.Update(ctx, &revoked); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	second := &Token{ID: "id-2", Name: "second", Hash: "same-hash"}
	if err := repo.Create(ctx, second); !errors.Is(err, ErrHashTaken) {
		t.Fatalf("Create() error = %v, want ErrHashTaken; the hash index has no partial WHERE, unlike name's", err)
	}
}
