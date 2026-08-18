package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getorcal/orcal/internal/id"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/google/uuid"
)

const runtimeName = "docker"

type CreateOptions struct {
	Name      string
	Image     string
	Env       map[string]string
	Labels    map[string]string
	Resources Resources
}

type SnapshotLookup interface {
	Resolve(ctx context.Context, ref string) (string, string, error)
}

type Service struct {
	repo      Repo
	rt        runtime.Runtime
	defaults  Resources
	network   string
	locks     *keyedMutex
	now       func() time.Time
	newID     func() string
	snapshots SnapshotLookup
}

func NewService(repo Repo, rt runtime.Runtime, defaults Resources, network string) *Service {
	return &Service{
		repo:     repo,
		rt:       rt,
		defaults: defaults,
		network:  network,
		locks:    newKeyedMutex(),
		now:      func() time.Time { return time.Now().UTC() },
		newID:    id.New,
	}
}

func (s *Service) SetSnapshots(l SnapshotLookup) { s.snapshots = l }

func (s *Service) Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	if opts.Image == "" {
		return nil, fmt.Errorf("%w: image is required", ErrInvalidImage)
	}
	if opts.Name != "" {
		if err := ValidateName(opts.Name); err != nil {
			return nil, err
		}
	}
	if err := validateResources(opts.Resources); err != nil {
		return nil, err
	}

	res := opts.Resources
	if res.CPUMillis == 0 {
		res.CPUMillis = s.defaults.CPUMillis
	}
	if res.MemoryBytes == 0 {
		res.MemoryBytes = s.defaults.MemoryBytes
	}
	if res.PidsLimit == 0 {
		res.PidsLimit = s.defaults.PidsLimit
	}

	now := s.now()
	sb := &Sandbox{
		ID:        s.newID(),
		Name:      opts.Name,
		Image:     opts.Image,
		State:     StateCreating,
		Runtime:   runtimeName,
		Resources: res,
		Env:       opts.Env,
		Labels:    opts.Labels,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if sb.Env == nil {
		sb.Env = map[string]string{}
	}
	if sb.Labels == nil {
		sb.Labels = map[string]string{}
	}

	unlock := s.locks.lock(sb.ID)
	defer unlock()

	if err := s.repo.Create(ctx, sb); err != nil {
		return nil, err
	}

	runtimeID, err := s.rt.Create(ctx, runtime.CreateSpec{
		SandboxID:   sb.ID,
		Image:       sb.Image,
		Env:         sb.Env,
		Labels:      sb.Labels,
		CPUMillis:   res.CPUMillis,
		MemoryBytes: res.MemoryBytes,
		PidsLimit:   res.PidsLimit,
		NetworkName: s.network,
	})
	if err != nil {
		return nil, s.markError(ctx, sb, err)
	}

	sb.RuntimeID = runtimeID
	if err := s.rt.Start(ctx, runtimeID); err != nil {
		return nil, s.markError(ctx, sb, err)
	}

	status, err := s.rt.Inspect(ctx, runtimeID)
	if err != nil {
		return nil, s.markError(ctx, sb, err)
	}
	if status.State != runtime.ContainerRunning {
		return nil, s.markError(ctx, sb, fmt.Errorf(
			"%w: the container for image %q exited immediately after start", ErrInvalidImage, sb.Image))
	}

	sb.State = StateRunning
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

func validateResources(r Resources) error {
	switch {
	case r.CPUMillis < 0:
		return fmt.Errorf("%w: cpu_millis must not be negative", ErrInvalidResources)
	case r.MemoryBytes < 0:
		return fmt.Errorf("%w: memory_bytes must not be negative", ErrInvalidResources)
	case r.PidsLimit < 0:
		return fmt.Errorf("%w: pids_limit must not be negative", ErrInvalidResources)
	}
	return nil
}

func (s *Service) markError(ctx context.Context, sb *Sandbox, cause error) error {
	sb.State = StateError
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *Service) Get(ctx context.Context, ref string) (*Sandbox, error) {
	if _, err := uuid.Parse(ref); err == nil {
		return s.repo.Get(ctx, ref)
	}
	return s.repo.GetByName(ctx, ref)
}

func (s *Service) List(ctx context.Context, f Filter) ([]*Sandbox, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Start(ctx context.Context, ref string) (*Sandbox, error) {
	return s.transition(ctx, ref, StateRunning, func(ctx context.Context, sb *Sandbox) error {
		return s.rt.Start(ctx, sb.RuntimeID)
	})
}

func (s *Service) Stop(ctx context.Context, ref string) (*Sandbox, error) {
	return s.transition(ctx, ref, StateStopped, func(ctx context.Context, sb *Sandbox) error {
		return s.rt.Stop(ctx, sb.RuntimeID, 10*time.Second)
	})
}

func (s *Service) Destroy(ctx context.Context, ref string) (*Sandbox, error) {
	sb, err := s.Get(ctx, ref)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.lock(sb.ID)
	defer unlock()

	sb, err = s.repo.Get(ctx, sb.ID)
	if err != nil {
		return nil, err
	}
	if !CanTransition(sb.State, StateDestroying) {
		return nil, fmt.Errorf("%w: %s cannot be destroyed", ErrInvalidState, sb.State)
	}

	sb.State = StateDestroying
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}

	if sb.RuntimeID != "" {
		if err := s.rt.Destroy(ctx, sb.RuntimeID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
			return nil, s.markError(ctx, sb, err)
		}
	}

	sb.State = StateDestroyed
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

func (s *Service) transition(ctx context.Context, ref string, target State, act func(context.Context, *Sandbox) error) (*Sandbox, error) {
	sb, err := s.Get(ctx, ref)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.lock(sb.ID)
	defer unlock()

	sb, err = s.repo.Get(ctx, sb.ID)
	if err != nil {
		return nil, err
	}
	if !CanTransition(sb.State, target) {
		return nil, fmt.Errorf("%w: %s cannot become %s", ErrInvalidState, sb.State, target)
	}
	if err := act(ctx, sb); err != nil {
		return nil, s.markError(ctx, sb, err)
	}

	sb.State = target
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

func (s *Service) RuntimeID(ctx context.Context, sandboxID string) (string, error) {
	sb, err := s.repo.Get(ctx, sandboxID)
	if err != nil {
		return "", err
	}
	if sb.State != StateRunning {
		return "", fmt.Errorf("%w: sandbox is %s", ErrInvalidState, sb.State)
	}
	return sb.RuntimeID, nil
}

func (s *Service) WithSnapshotSource(ctx context.Context, ref string, fn func(snapshot.SandboxSource) error) error {
	sb, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}

	unlock := s.locks.lock(sb.ID)
	defer unlock()

	sb, err = s.repo.Get(ctx, sb.ID)
	if err != nil {
		return err
	}
	if sb.State != StateRunning && sb.State != StateStopped {
		return fmt.Errorf("%w: cannot snapshot a sandbox that is %s", ErrInvalidState, sb.State)
	}

	return fn(snapshot.SandboxSource{
		ID:               sb.ID,
		RuntimeID:        sb.RuntimeID,
		Image:            sb.Image,
		ParentSnapshotID: sb.ParentSnapshotID,
	})
}

func (s *Service) Fork(ctx context.Context, snapshotRef string, opts CreateOptions) (*Sandbox, error) {
	if s.snapshots == nil {
		return nil, ErrSnapshotRequired
	}
	runtimeRef, snapshotID, err := s.snapshots.Resolve(ctx, snapshotRef)
	if err != nil {
		return nil, err
	}

	opts.Image = runtimeRef
	created, err := s.Create(ctx, opts)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.lock(created.ID)
	defer unlock()

	created.ParentSnapshotID = &snapshotID
	created.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, created); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) Restore(ctx context.Context, sandboxRef, snapshotRef string) (*Sandbox, error) {
	if s.snapshots == nil {
		return nil, ErrSnapshotRequired
	}
	runtimeRef, snapshotID, err := s.snapshots.Resolve(ctx, snapshotRef)
	if err != nil {
		return nil, err
	}

	sb, err := s.Get(ctx, sandboxRef)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.lock(sb.ID)
	defer unlock()

	sb, err = s.repo.Get(ctx, sb.ID)
	if err != nil {
		return nil, err
	}
	if sb.State != StateRunning && sb.State != StateStopped {
		return nil, fmt.Errorf("%w: cannot restore a sandbox that is %s", ErrInvalidState, sb.State)
	}

	if sb.RuntimeID != "" {
		if err := s.rt.Destroy(ctx, sb.RuntimeID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
			return nil, s.markError(ctx, sb, err)
		}
	}

	runtimeID, err := s.rt.Create(ctx, runtime.CreateSpec{
		SandboxID:   sb.ID,
		Image:       runtimeRef,
		Env:         sb.Env,
		Labels:      sb.Labels,
		CPUMillis:   sb.Resources.CPUMillis,
		MemoryBytes: sb.Resources.MemoryBytes,
		PidsLimit:   sb.Resources.PidsLimit,
		NetworkName: s.network,
	})
	if err != nil {
		return nil, s.markError(ctx, sb, err)
	}

	sb.RuntimeID = runtimeID
	if err := s.rt.Start(ctx, runtimeID); err != nil {
		return nil, s.markError(ctx, sb, err)
	}

	status, err := s.rt.Inspect(ctx, runtimeID)
	if err != nil {
		return nil, s.markError(ctx, sb, err)
	}
	if status.State != runtime.ContainerRunning {
		return nil, s.markError(ctx, sb, fmt.Errorf("%w: container exited immediately after restore", ErrInvalidState))
	}

	sb.State = StateRunning
	sb.ParentSnapshotID = &snapshotID
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}
