package auth

import (
	"context"
	"sync"
	"time"
)

type MemoryRepo struct {
	mu     sync.Mutex
	tokens []*Token
}

func NewMemoryRepo() *MemoryRepo { return &MemoryRepo{} }

func (m *MemoryRepo) Create(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.tokens {
		if existing.Name == t.Name && existing.RevokedAt == nil {
			return ErrNameTaken
		}
	}
	clone := *t
	m.tokens = append(m.tokens, &clone)
	return nil
}

func (m *MemoryRepo) Get(_ context.Context, id string) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.ID == id {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) GetByHash(_ context.Context, hash string) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.Hash == hash {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) GetByName(_ context.Context, name string) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.Name == name && t.RevokedAt == nil {
			clone := *t
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) List(_ context.Context) ([]*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Token, 0, len(m.tokens))
	for _, t := range m.tokens {
		clone := *t
		out = append(out, &clone)
	}
	return out, nil
}

func (m *MemoryRepo) Update(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.tokens {
		if existing.ID == t.ID {
			clone := *t
			m.tokens[i] = &clone
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryRepo) TouchLastUsed(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.ID == id {
			stamp := at
			t.LastUsedAt = &stamp
			return nil
		}
	}
	return ErrNotFound
}
