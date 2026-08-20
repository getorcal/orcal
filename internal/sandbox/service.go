package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/getorcal/orcal/internal/id"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/snapshot"
)

const runtimeName = "docker"

type CreateOptions struct {
	Name      string
	Image     string
	Env       map[string]string
	Labels    map[string]string
	Resources Resources
	Network   Network
}

// SnapshotLookup is the mirror of snapshot.SandboxAccess: each package declares the slice of
// the other it consumes, so the dependency runs one way only (sandbox imports snapshot).
// It is wired after construction via SetSnapshots because the two services are mutually
// dependent; Fork and Restore return ErrSnapshotRequired if that wiring was skipped.
type SnapshotLookup interface {
	Resolve(ctx context.Context, ref string) (snapshot.Resolved, error)
}

type Networks struct {
	Full     string
	Isolated string
}

func (n Networks) nameFor(mode Network) string {
	if mode == NetworkNone {
		return n.Isolated
	}
	return n.Full
}

type Service struct {
	repo       Repo
	rt         runtime.Runtime
	defaults   Resources
	networks   Networks
	ociRuntime string
	locks      *keyedMutex
	now        func() time.Time
	newID      func() string
	snapshots  SnapshotLookup
}

func NewService(repo Repo, rt runtime.Runtime, defaults Resources, networks Networks, ociRuntime string) *Service {
	return &Service{
		repo:       repo,
		rt:         rt,
		defaults:   defaults,
		networks:   networks,
		ociRuntime: ociRuntime,
		locks:      newKeyedMutex(),
		now:        func() time.Time { return time.Now().UTC() },
		newID:      id.New,
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
	if opts.Network == "" {
		opts.Network = NetworkFull
	}
	if err := ValidateNetwork(opts.Network); err != nil {
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
		ID:         s.newID(),
		Name:       opts.Name,
		Image:      opts.Image,
		State:      StateCreating,
		Runtime:    runtimeName,
		OCIRuntime: s.ociRuntime,
		Resources:  res,
		Env:        opts.Env,
		Labels:     opts.Labels,
		Network:    opts.Network,
		CreatedAt:  now,
		UpdatedAt:  now,
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
		NetworkName: s.networks.nameFor(opts.Network),
		OCIRuntime:  s.ociRuntime,
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

func (s *Service) RuntimeIDFor(ctx context.Context, ref string) (string, error) {
	sb, err := s.Get(ctx, ref)
	if err != nil {
		return "", err
	}
	if err := fileOperable(sb); err != nil {
		return "", err
	}
	return sb.RuntimeID, nil
}

func (s *Service) WithLockedRuntimeID(ctx context.Context, ref string, fn func(runtimeID string) error) error {
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
	if err := fileOperable(sb); err != nil {
		return err
	}
	return fn(sb.RuntimeID)
}

func fileOperable(sb *Sandbox) error {
	if sb.State != StateRunning && sb.State != StateStopped {
		return fmt.Errorf("%w: cannot access files on a sandbox that is %s", ErrInvalidState, sb.State)
	}
	if sb.RuntimeID == "" {
		return fmt.Errorf("%w: sandbox has no container", ErrInvalidState)
	}
	return nil
}

// WithSnapshotSource runs fn while holding the per-sandbox lock, so the Docker commit and the
// snapshot row are written atomically with respect to any other mutation of this sandbox.
// A plain getter would let a Destroy land between reading the runtime ID and committing it.
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
		Network:          string(sb.Network),
	})
}

func (s *Service) Fork(ctx context.Context, snapshotRef string, opts CreateOptions) (*Sandbox, error) {
	if s.snapshots == nil {
		return nil, ErrSnapshotRequired
	}
	resolved, err := s.snapshots.Resolve(ctx, snapshotRef)
	if err != nil {
		return nil, err
	}

	if opts.Network == "" && resolved.Network != "" {
		opts.Network = Network(resolved.Network)
	}
	opts.Image = resolved.RuntimeRef
	created, err := s.Create(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Create released the per-sandbox lock before returning, so `created` is already stale.
	// repo.Update rewrites every column, meaning a Destroy that landed in that window would be
	// silently reverted to state=running with a dead runtime_id. Re-read under the lock instead.
	unlock := s.locks.lock(created.ID)
	defer unlock()

	fresh, err := s.repo.Get(ctx, created.ID)
	if err != nil {
		return nil, err
	}
	fresh.ParentSnapshotID = &resolved.ID
	fresh.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

func (s *Service) Restore(ctx context.Context, sandboxRef, snapshotRef string) (*Sandbox, error) {
	if s.snapshots == nil {
		return nil, ErrSnapshotRequired
	}
	resolved, err := s.snapshots.Resolve(ctx, snapshotRef)
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
		Image:       resolved.RuntimeRef,
		Env:         sb.Env,
		Labels:      sb.Labels,
		CPUMillis:   sb.Resources.CPUMillis,
		MemoryBytes: sb.Resources.MemoryBytes,
		PidsLimit:   sb.Resources.PidsLimit,
		NetworkName: s.networks.nameFor(sb.Network),
		OCIRuntime:  s.ociRuntime,
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
	sb.OCIRuntime = s.ociRuntime
	sb.ParentSnapshotID = &resolved.ID
	sb.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

// UnpausePaused recovers containers stranded by a daemon that died between pause and commit.
// Nothing else can unpause them, and until they are the sandbox reads as running while every
// exec against it hangs.
func (s *Service) UnpausePaused(ctx context.Context) (int, error) {
	all, err := s.repo.List(ctx, Filter{States: []State{StateRunning}, Limit: 0})
	if err != nil {
		return 0, err
	}

	unpaused := 0
	for _, sb := range all {
		if sb.RuntimeID == "" {
			continue
		}
		// Boot reconciliation must not abort on one unreachable container, so per-sandbox
		// failures are skipped rather than returned; the count reports what actually recovered.
		status, err := s.rt.Inspect(ctx, sb.RuntimeID)
		if err != nil || status.State != runtime.ContainerPaused {
			continue
		}
		if err := s.rt.Unpause(ctx, sb.RuntimeID); err == nil {
			unpaused++
		}
	}
	return unpaused, nil
}
