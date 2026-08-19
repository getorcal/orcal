package snapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/getorcal/orcal/internal/runtime"
)

type SandboxSource struct {
	ID               string
	RuntimeID        string
	Image            string
	ParentSnapshotID *string
}

// SandboxAccess is declared here, in the consuming package, rather than importing
// internal/sandbox — which imports this package for Fork and Restore. Keeping this interface
// to strings and a plain struct is what stops the two from forming an import cycle.
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
		// The image exists before the row does, so a failed insert would orphan it on the host
		// with nothing pointing at it. Roll the image back and report both failures.
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

// Get accepts an ID or a name. The two namespaces cannot collide because ValidateName rejects
// UUID-shaped names at creation time, so a parseable ref is always an ID.
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

	// Image before row: if the runtime refuses (Docker returns a conflict while a container
	// still references the image) the row must survive, or the image is orphaned. An image
	// already removed out of band is tolerated so it cannot block cleanup forever.
	if err := s.rt.DeleteSnapshot(ctx, snap.RuntimeRef); err != nil {
		if !errors.Is(err, runtime.ErrNotFound) {
			return err
		}
	}
	return s.repo.Delete(ctx, snap.ID)
}
