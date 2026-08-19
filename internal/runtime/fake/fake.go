package fake

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/getorcal/orcal/internal/runtime"
)

type Step struct {
	Frame runtime.Frame
	Err   error
}

type container struct {
	spec  runtime.CreateSpec
	state runtime.ContainerState
	files map[string]*fileNode
}

type Fake struct {
	mu             sync.Mutex
	containers     map[string]*container
	sessions       map[string]*session
	script         []Step
	exitCode       int
	createErr      error
	execErr        error
	exitAfterStart bool
	lastCreateSpec runtime.CreateSpec
	snapshots      map[string]int64
	snapshotErr    error
	snapshotSeq    int
}

func New() *Fake {
	return &Fake{
		containers: make(map[string]*container),
		sessions:   make(map[string]*session),
		snapshots:  make(map[string]int64),
	}
}

func (f *Fake) SetExecScript(script []Step, exitCode int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = script
	f.exitCode = exitCode
}

func (f *Fake) SetCreateError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createErr = err
}

func (f *Fake) SetExecError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execErr = err
}

func (f *Fake) SetExitAfterStart(exits bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitAfterStart = exits
}

func (f *Fake) SetSnapshotError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshotErr = err
}

func (f *Fake) ForcePaused(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[id]; ok {
		c.state = runtime.ContainerPaused
	}
}

func (f *Fake) LastCreateSpec() runtime.CreateSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCreateSpec
}

func (f *Fake) IDForSandbox(sandboxID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, c := range f.containers {
		if c.spec.SandboxID == sandboxID {
			return id
		}
	}
	return ""
}

func (f *Fake) Create(ctx context.Context, spec runtime.CreateSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.lastCreateSpec = spec
	id := uuid.NewString()
	f.containers[id] = &container{spec: spec, state: runtime.ContainerStopped}
	return id, nil
}

func (f *Fake) Start(ctx context.Context, id string) error {
	f.mu.Lock()
	exits := f.exitAfterStart
	f.mu.Unlock()
	if exits {
		return f.transition(id, runtime.ContainerStopped)
	}
	return f.transition(id, runtime.ContainerRunning)
}

func (f *Fake) Stop(ctx context.Context, id string, timeout time.Duration) error {
	return f.transition(id, runtime.ContainerStopped)
}

func (f *Fake) Destroy(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return nil
	}
	c.state = runtime.ContainerGone
	return nil
}

func (f *Fake) transition(id string, state runtime.ContainerState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok || c.state == runtime.ContainerGone {
		return fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	c.state = state
	return nil
}

func (f *Fake) Inspect(ctx context.Context, id string) (runtime.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return runtime.Status{State: runtime.ContainerGone}, nil
	}
	return runtime.Status{State: c.state}, nil
}

func (f *Fake) Exec(ctx context.Context, id string, spec runtime.ExecSpec) (runtime.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.execErr != nil {
		return nil, f.execErr
	}
	c, ok := f.containers[id]
	if !ok {
		return nil, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	if c.state != runtime.ContainerRunning {
		return nil, fmt.Errorf("%w: container %s is %s", runtime.ErrConflict, id, c.state)
	}
	s := &session{id: uuid.NewString(), steps: f.script, exitCode: f.exitCode}
	f.sessions[s.id] = s
	return s, nil
}

func (f *Fake) InspectExec(ctx context.Context, execID string) (runtime.ExecStatus, error) {
	f.mu.Lock()
	s, ok := f.sessions[execID]
	f.mu.Unlock()
	if !ok {
		return runtime.ExecStatus{}, fmt.Errorf("%w: exec %s", runtime.ErrNotFound, execID)
	}
	return s.status(), nil
}

func (f *Fake) Snapshot(ctx context.Context, id string) (runtime.SnapshotInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.containers[id]
	if !ok || c.state == runtime.ContainerGone {
		return runtime.SnapshotInfo{}, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}

	// The fake pauses and restores exactly as the Docker adapter does, including on the failure
	// path. A fake more forgiving than the real runtime would make every test above it prove
	// less than it appears to.
	restore := c.state
	if c.state == runtime.ContainerRunning {
		c.state = runtime.ContainerPaused
	}
	defer func() { c.state = restore }()

	if f.snapshotErr != nil {
		return runtime.SnapshotInfo{}, f.snapshotErr
	}

	f.snapshotSeq++
	ref := fmt.Sprintf("sha256:fake%d", f.snapshotSeq)
	f.snapshots[ref] = 1024
	return runtime.SnapshotInfo{Ref: ref, SizeBytes: 1024}, nil
}

func (f *Fake) DeleteSnapshot(ctx context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.snapshots[ref]; !ok {
		return fmt.Errorf("%w: snapshot %s", runtime.ErrNotFound, ref)
	}
	for _, c := range f.containers {
		if c.spec.Image == ref && c.state != runtime.ContainerGone {
			return fmt.Errorf("%w: snapshot %s is in use", runtime.ErrConflict, ref)
		}
	}
	delete(f.snapshots, ref)
	return nil
}

func (f *Fake) Unpause(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok || c.state == runtime.ContainerGone {
		return fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	if c.state == runtime.ContainerPaused {
		c.state = runtime.ContainerRunning
	}
	return nil
}

type session struct {
	mu       sync.Mutex
	id       string
	steps    []Step
	pos      int
	exitCode int
	done     bool
}

func (s *session) ID() string { return s.id }

func (s *session) Recv() (runtime.Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pos >= len(s.steps) {
		return runtime.Frame{}, io.EOF
	}
	step := s.steps[s.pos]
	s.pos++
	if step.Err != nil {
		return runtime.Frame{}, step.Err
	}
	return step.Frame, nil
}

func (s *session) Wait(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	return s.exitCode, nil
}

func (s *session) Close() error { return nil }

func (s *session) status() runtime.ExecStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.done {
		return runtime.ExecStatus{Running: true}
	}
	code := s.exitCode
	return runtime.ExecStatus{Running: false, ExitCode: &code}
}
