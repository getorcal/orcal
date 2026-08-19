package audit

import (
	"context"
	"slices"
	"sync"
	"time"
)

type MemoryRepo struct {
	mu     sync.Mutex
	events []*Event
}

func NewMemoryRepo() *MemoryRepo { return &MemoryRepo{} }

func (m *MemoryRepo) Create(_ context.Context, e *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *e
	m.events = append(m.events, &clone)
	return nil
}

func (m *MemoryRepo) List(_ context.Context, f Filter) ([]*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Event, 0, len(m.events))
	for _, e := range slices.Backward(m.events) {
		if f.Actor != "" && e.ActorTokenID != f.Actor {
			continue
		}
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		if f.ResourceType != "" && e.ResourceType != f.ResourceType {
			continue
		}
		if f.ResourceID != "" && e.ResourceID != f.ResourceID {
			continue
		}
		if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && !e.Timestamp.Before(f.Until) {
			continue
		}
		if f.Cursor != "" && e.ID >= f.Cursor {
			continue
		}
		clone := *e
		out = append(out, &clone)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryRepo) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.events[:0]
	var removed int64
	for _, e := range m.events {
		if e.Timestamp.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	m.events = kept
	return removed, nil
}

func (m *MemoryRepo) DeleteBeyondCount(_ context.Context, keep int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) <= keep {
		return 0, nil
	}
	removed := int64(len(m.events) - keep)
	m.events = m.events[len(m.events)-keep:]
	return removed, nil
}
