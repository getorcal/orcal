package snapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/google/uuid"
)

type SandboxSource struct {
	ID               string
	RuntimeID        string
	Image            string
	ParentSnapshotID *string
}

type SandboxAccess interface {
	WithSnapshotSource(ctx context.Context, ref string, fn func(SandboxSource) error) error
}

type CreateOptions struct {
	SandboxRef string
	Name       string
}

type Service struct {
	repo      Repo
	sandboxes SandboxAccess
	rt        runtime.Runtime
	now       func() time.Time
	newID     func() string
}

func NewService(repo Repo, sandboxes SandboxAccess, rt runtime.Runtime) *Service {
	return &Service{
		repo:      repo,
		sandboxes: sandboxes,
		rt:        rt,
		now:       func() time.Time { return time.Now().UTC() },
		newID:     NewID,
	}
}

func (s *Service) Create(ctx context.Context, opts CreateOptions) (*Snapshot, error) {
	if opts.Name != "" {
		if err := ValidateName(opts.Name); err != nil {
			return nil, err
		}
	}

	var created *Snapshot
	err := s.sandboxes.WithSnapshotSource(ctx, opts.SandboxRef, func(src SandboxSource) error {
		info, err := s.rt.Snapshot(ctx, src.RuntimeID)
		if err != nil {
			return err
		}

		snap := &Snapshot{
			ID:         s.newID(),
			Name:       opts.Name,
			SandboxID:  src.ID,
			ParentID:   src.ParentSnapshotID,
			RuntimeRef: info.Ref,
			Image:      src.Image,
			SizeBytes:  info.SizeBytes,
			CreatedAt:  s.now(),
		}
		if err := s.repo.Create(ctx, snap); err != nil {
			return errors.Join(err, s.rt.DeleteSnapshot(ctx, info.Ref))
		}
		created = snap
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, ref string) (*Snapshot, error) {
	if _, err := uuid.Parse(ref); err == nil {
		return s.repo.Get(ctx, ref)
	}
	return s.repo.GetByName(ctx, ref)
}

func (s *Service) List(ctx context.Context, f Filter) ([]*Snapshot, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Resolve(ctx context.Context, ref string) (string, string, error) {
	snap, err := s.Get(ctx, ref)
	if err != nil {
		return "", "", err
	}
	return snap.RuntimeRef, snap.ID, nil
}

func (s *Service) Delete(ctx context.Context, ref string) error {
	snap, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}

	children, err := s.repo.CountChildren(ctx, snap.ID)
	if err != nil {
		return err
	}
	if children > 0 {
		return fmt.Errorf("%w: %d descend from it", ErrHasChildren, children)
	}

	if err := s.rt.DeleteSnapshot(ctx, snap.RuntimeRef); err != nil {
		if !errors.Is(err, runtime.ErrNotFound) {
			return err
		}
	}
	return s.repo.Delete(ctx, snap.ID)
}
