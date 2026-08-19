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

func TestEventsCursorContinuesStrictlyOlderWithNoOverlap(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	ids := []string{
		"0192f3a4-5b6c-7d8e-9f01-000000000001",
		"0192f3a4-5b6c-7d8e-9f01-000000000002",
		"0192f3a4-5b6c-7d8e-9f01-000000000003",
		"0192f3a4-5b6c-7d8e-9f01-000000000004",
		"0192f3a4-5b6c-7d8e-9f01-000000000005",
	}
	for i, eid := range ids {
		e := &audit.Event{
			ID: eid, Timestamp: now.Add(time.Duration(i) * time.Second), ActorTokenID: "tok-1", ActorName: "ci",
			Action: audit.ActionSandboxCreate, ResourceType: "sandbox", ResourceID: "sb-1",
			RequestID: "req-1", Status: 201, RemoteAddr: "10.0.0.1:5555",
			Details: map[string]any{"image": "alpine"},
		}
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	first, err := repo.List(ctx, audit.Filter{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 2 || first[0].ID != ids[4] || first[1].ID != ids[3] {
		t.Fatalf("unexpected first page: %+v", first)
	}

	second, err := repo.List(ctx, audit.Filter{Limit: 2, Cursor: first[len(first)-1].ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[1] {
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

	third, err := repo.List(ctx, audit.Filter{Limit: 2, Cursor: second[len(second)-1].ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(third) != 1 || third[0].ID != ids[0] {
		t.Fatalf("unexpected third page: %+v", third)
	}
}

func TestPruneByCountKeepsTheNewest(t *testing.T) {
	repo, ctx := newEventRepo(t)
	now := time.Now().UTC()
	seeded := make([]*audit.Event, 0, 11)
	for i := range 11 {
		seeded = append(seeded, seedEvent(t, repo, ctx, audit.ActionSandboxCreate, now.Add(time.Duration(i)*time.Second)))
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

	remainingIDs := make(map[string]bool, len(remaining))
	for _, e := range remaining {
		remainingIDs[e.ID] = true
	}
	if remainingIDs[seeded[0].ID] {
		t.Fatalf("expected oldest event %s to be pruned, survivors: %v", seeded[0].ID, remainingIDs)
	}
	for _, want := range seeded[1:] {
		if !remainingIDs[want.ID] {
			t.Fatalf("expected newest event %s to survive prune, survivors: %v", want.ID, remainingIDs)
		}
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
