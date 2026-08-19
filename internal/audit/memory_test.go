package audit

import (
	"context"
	"testing"
	"time"
)

func seedMemEvent(t *testing.T, repo *MemoryRepo, id string, at time.Time) *Event {
	t.Helper()
	e := &Event{
		ID: id, Timestamp: at, ActorTokenID: "tok-1", ActorName: "ci",
		Action: ActionSandboxCreate, ResourceType: "sandbox", ResourceID: "sb-1",
		RequestID: "req-1", Status: 201, RemoteAddr: "10.0.0.1:5555",
	}
	if err := repo.Create(context.Background(), e); err != nil {
		t.Fatalf("create: %v", err)
	}
	return e
}

func TestMemoryRepoListIsNewestFirstRegardlessOfInsertOrder(t *testing.T) {
	repo := NewMemoryRepo()
	now := time.Now().UTC()
	seedMemEvent(t, repo, "evt-02", now)
	seedMemEvent(t, repo, "evt-03", now)
	seedMemEvent(t, repo, "evt-01", now)

	got, err := repo.List(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].ID != "evt-03" || got[1].ID != "evt-02" || got[2].ID != "evt-01" {
		t.Fatalf("expected newest-first by id regardless of insert order, got %v", got)
	}
}

func TestMemoryRepoListCursorContinuesStrictlyOlderWithNoOverlap(t *testing.T) {
	repo := NewMemoryRepo()
	now := time.Now().UTC()
	ids := []string{"evt-01", "evt-02", "evt-03", "evt-04", "evt-05"}
	for _, id := range ids {
		seedMemEvent(t, repo, id, now)
	}

	first, err := repo.List(context.Background(), Filter{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 2 || first[0].ID != "evt-05" || first[1].ID != "evt-04" {
		t.Fatalf("unexpected first page: %+v", first)
	}

	second, err := repo.List(context.Background(), Filter{Limit: 2, Cursor: first[len(first)-1].ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(second) != 2 || second[0].ID != "evt-03" || second[1].ID != "evt-02" {
		t.Fatalf("unexpected second page: %+v", second)
	}

	seen := map[string]bool{}
	for _, e := range first {
		seen[e.ID] = true
	}
	for _, e := range second {
		if seen[e.ID] {
			t.Fatalf("page overlap on id %s", e.ID)
		}
	}

	third, err := repo.List(context.Background(), Filter{Limit: 2, Cursor: second[len(second)-1].ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(third) != 1 || third[0].ID != "evt-01" {
		t.Fatalf("unexpected third page: %+v", third)
	}
}

func TestMemoryRepoDeleteBeyondCountKeepsNewestByID(t *testing.T) {
	repo := NewMemoryRepo()
	now := time.Now().UTC()
	ids := []string{"evt-05", "evt-01", "evt-04", "evt-02", "evt-03"}
	for _, id := range ids {
		seedMemEvent(t, repo, id, now)
	}

	removed, err := repo.DeleteBeyondCount(context.Background(), 3)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}

	got, err := repo.List(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].ID != "evt-05" || got[1].ID != "evt-04" || got[2].ID != "evt-03" {
		t.Fatalf("expected newest three to survive prune, got %+v", got)
	}
}
