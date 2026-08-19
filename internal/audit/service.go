package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/getorcal/orcal/internal/id"
)

type RetentionPolicy struct {
	MaxAge    time.Duration
	MaxEvents int
}

type Service struct {
	repo      Repo
	retention RetentionPolicy
	now       func() time.Time
	newID     func() string
}

func NewService(repo Repo, retention RetentionPolicy) *Service {
	return &Service{
		repo:      repo,
		retention: retention,
		now:       func() time.Time { return time.Now().UTC() },
		newID:     id.New,
	}
}

func (s *Service) Record(ctx context.Context, e *Event) error {
	if !ValidAction(e.Action) {
		return fmt.Errorf("audit: unknown action %q", e.Action)
	}
	if e.ID == "" {
		e.ID = s.newID()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = s.now()
	}
	return s.repo.Create(ctx, e)
}

func (s *Service) List(ctx context.Context, f Filter) ([]*Event, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Prune(ctx context.Context) (int64, error) {
	var total int64
	if s.retention.MaxAge > 0 {
		removed, err := s.repo.DeleteOlderThan(ctx, s.now().Add(-s.retention.MaxAge))
		if err != nil {
			return total, err
		}
		total += removed
	}
	if s.retention.MaxEvents > 0 {
		removed, err := s.repo.DeleteBeyondCount(ctx, s.retention.MaxEvents)
		if err != nil {
			return total, err
		}
		total += removed
	}
	return total, nil
}
