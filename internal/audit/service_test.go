package audit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestKnownActionsIsClosedAtSixteen(t *testing.T) {
	if len(KnownActions()) != 16 {
		t.Fatalf("expected 16 actions, got %d", len(KnownActions()))
	}
	if ValidAction("sandbox.delete") {
		t.Fatal("an invented action must not validate")
	}
	if !ValidAction(ActionAuthDenied) {
		t.Fatal("auth.denied must be a known action")
	}
}

func TestRecordRejectsAnUnknownAction(t *testing.T) {
	svc := NewService(newMemEvents(), RetentionPolicy{MaxAge: time.Hour, MaxEvents: 10})
	err := svc.Record(context.Background(), &Event{Action: "made.up"})
	if err == nil {
		t.Fatal("an unknown action must be refused so the vocabulary stays closed")
	}
}

func TestRecordStampsIDAndTimestamp(t *testing.T) {
	repo := newMemEvents()
	svc := NewService(repo, RetentionPolicy{MaxAge: time.Hour, MaxEvents: 10})
	if err := svc.Record(context.Background(), &Event{Action: ActionSandboxCreate}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected one event, got %d", len(repo.events))
	}
	if repo.events[0].ID == "" {
		t.Fatal("Record must assign an id")
	}
	if repo.events[0].Timestamp.IsZero() {
		t.Fatal("Record must assign a timestamp")
	}
}

func TestPruneEnforcesBothAxes(t *testing.T) {
	repo := newMemEvents()
	svc := NewService(repo, RetentionPolicy{MaxAge: 24 * time.Hour, MaxEvents: 10})
	ctx := context.Background()

	removed, err := svc.Prune(ctx)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 0 {
		t.Fatalf("an empty log must prune nothing, got %d", removed)
	}
	if repo.byAge != 1 || repo.byCount != 1 {
		t.Fatalf("prune must apply both axes every run, got age=%d count=%d", repo.byAge, repo.byCount)
	}
}

func TestPruneIsDisabledByZeroPolicy(t *testing.T) {
	repo := newMemEvents()
	svc := NewService(repo, RetentionPolicy{})
	if _, err := svc.Prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if repo.byAge != 0 || repo.byCount != 0 {
		t.Fatal("a zero policy must not delete anything")
	}
}

type memEvents struct {
	mu      sync.Mutex
	events  []*Event
	byAge   int
	byCount int
}

func newMemEvents() *memEvents {
	return &memEvents{}
}

func (m *memEvents) Create(_ context.Context, e *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *e
	m.events = append(m.events, &clone)
	return nil
}

func (m *memEvents) List(_ context.Context, f Filter) ([]*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Event, 0, len(m.events))
	for _, e := range m.events {
		clone := *e
		out = append(out, &clone)
	}
	return out, nil
}

func (m *memEvents) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byAge++
	return 0, nil
}

func (m *memEvents) DeleteBeyondCount(_ context.Context, keep int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byCount++
	return 0, nil
}
