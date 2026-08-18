package fake

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/google/uuid"
)

type Step struct {
	Frame runtime.Frame
	Err   error
}

type container struct {
	spec  runtime.CreateSpec
	state runtime.ContainerState
}

type Fake struct {
	mu             sync.Mutex
	containers     map[string]*container
	sessions       map[string]*session
	script         []Step
	exitCode       int
	createErr      error
	execErr        error
	lastCreateSpec runtime.CreateSpec
}

func New() *Fake {
	return &Fake{
		containers: make(map[string]*container),
		sessions:   make(map[string]*session),
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

func (f *Fake) LastCreateSpec() runtime.CreateSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCreateSpec
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
