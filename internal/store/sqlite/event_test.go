package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/id"
)

func newEventRepo(t *testing.T) (audit.Repo, context.Context) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store.Events(), context.Background()
}

func seedEvent(t *testing.T, repo audit.Repo, ctx context.Context, action audit.Action, at time.Time) *audit.Event {
	t.Helper()
	e := &audit.Event{
		ID: id.New(), Timestamp: at, ActorTokenID: "tok-1", ActorName: "ci",
		Action: action, ResourceType: "sandbox", ResourceID: "sb-1",
		RequestID: "req-1", Status: 201, RemoteAddr: "10.0.0.1:5555",
		Details: map[string]any{"image": "alpine"},
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("create: %v", err)
	}
	return e
}

func TestEventRoundTrip(t *testing.T) {
	repo, ctx := newEventRepo(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	want := seedEvent(t, repo, ctx, audit.ActionSandboxCreate, at)

	got, err := repo.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	if got[0].ID != want.ID || got[0].Action != audit.ActionSandboxCreate {
		t.Fatalf("round trip mismatch: %+v", got[0])
	}
	if got[0].Details["image"] != "alpine" {
		t.Fatalf("details did not survive: %v", got[0].Details)
	}
	if !got[0].Timestamp.Equal(at) {
		t.Fatalf("timestamp did not survive: %v", got[0].Timestamp)
	}
}

func TestEventsAreNewestFirst(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	first := seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now.Add(-2*time.Hour))
	time.Sleep(2 * time.Millisecond)
	last := seedEvent(t, repo, ctx, audit.ActionSandboxDestroy, now)

	got, err := repo.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two events, got %d", len(got))
	}
	if got[0].ID != last.ID || got[1].ID != first.ID {
		t.Fatal("events must come back newest-first")
	}
}

func TestEventFilters(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now.Add(-time.Hour))
	seedEvent(t, repo, ctx, audit.ActionExecCreate, now)

	byAction, err := repo.List(ctx, audit.Filter{Action: audit.ActionExecCreate})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byAction) != 1 || byAction[0].Action != audit.ActionExecCreate {
		t.Fatalf("action filter failed: %+v", byAction)
	}

	bySince, err := repo.List(ctx, audit.Filter{Since: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bySince) != 1 {
		t.Fatalf("since filter failed: got %d", len(bySince))
	}

	none, err := repo.List(ctx, audit.Filter{Actor: "nobody"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("actor filter failed: got %d", len(none))
	}
}

func TestPruneByCountKeepsTheNewest(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	for i := range 11 {
		seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now.Add(time.Duration(i)*time.Second))
		time.Sleep(time.Millisecond)
	}

	removed, err := repo.DeleteBeyondCount(ctx, 10)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one row removed, got %d", removed)
	}
	remaining, err := repo.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 10 {
		t.Fatalf("expected 10 remaining, got %d", len(remaining))
	}
}

func TestPruneByAge(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now.Add(-48*time.Hour))
	seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now)

	removed, err := repo.DeleteOlderThan(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one row removed, got %d", removed)
	}
}
