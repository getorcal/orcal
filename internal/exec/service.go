package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/google/uuid"
)

type SandboxLookup interface {
	RuntimeID(ctx context.Context, sandboxID string) (string, error)
}

type CreateOptions struct {
	SandboxID  string
	Command    []string
	Env        map[string]string
	WorkingDir string
	User       string
}

type Service struct {
	repo      Repo
	sandboxes SandboxLookup
	rt        runtime.Runtime
	dir       string
	maxBytes  int64
	bcast     *Broadcaster
	now       func() time.Time
	newID     func() string
	wg        sync.WaitGroup
}

func NewService(repo Repo, sandboxes SandboxLookup, rt runtime.Runtime, dir string, maxBytes int64) (*Service, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("exec: create log dir: %w", err)
	}
	return &Service{
		repo:      repo,
		sandboxes: sandboxes,
		rt:        rt,
		dir:       dir,
		maxBytes:  maxBytes,
		bcast:     NewBroadcaster(),
		now:       func() time.Time { return time.Now().UTC() },
		newID:     uuid.NewString,
	}, nil
}

func (s *Service) Broadcaster() *Broadcaster { return s.bcast }

func (s *Service) LogPath(id string) string { return filepath.Join(s.dir, id+".log") }

func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) Create(ctx context.Context, opts CreateOptions) (*Exec, error) {
	if len(opts.Command) == 0 {
		return nil, errors.New("exec: command is required")
	}
	runtimeID, err := s.sandboxes.RuntimeID(ctx, opts.SandboxID)
	if err != nil {
		return nil, err
	}

	e := &Exec{
		ID:         s.newID(),
		SandboxID:  opts.SandboxID,
		Command:    opts.Command,
		Env:        opts.Env,
		WorkingDir: opts.WorkingDir,
		User:       opts.User,
		State:      StateRunning,
		StartedAt:  s.now(),
	}
	if e.Env == nil {
		e.Env = map[string]string{}
	}
	if err := s.repo.Create(ctx, e); err != nil {
		return nil, err
	}

	session, err := s.rt.Exec(ctx, runtimeID, runtime.ExecSpec{
		Command:    opts.Command,
		Env:        opts.Env,
		WorkingDir: opts.WorkingDir,
		User:       opts.User,
	})
	if err != nil {
		return nil, s.markFailed(ctx, e, err)
	}

	e.RuntimeExecID = session.ID()
	if err := s.repo.Update(ctx, e); err != nil {
		session.Close()
		return nil, err
	}

	s.wg.Add(1)
	go s.supervise(e.ID, session)

	return e, nil
}

func (s *Service) markFailed(ctx context.Context, e *Exec, cause error) error {
	finished := s.now()
	e.State = StateFailed
	e.FinishedAt = &finished
	if err := s.repo.Update(ctx, e); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *Service) supervise(execID string, session runtime.Session) {
	defer s.wg.Done()
	defer session.Close()

	ctx := context.Background()

	writer, err := NewLogWriter(s.LogPath(execID), s.maxBytes)
	if err != nil {
		session.Wait(ctx)
		s.finish(ctx, execID, nil, 0, false, StateFailed)
		s.bcast.Notify(execID)
		return
	}
	defer writer.Close()

	var appendFailed bool
	for {
		frame, err := session.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				if _, gapErr := writer.Append(LogGap, nil); gapErr != nil {
					appendFailed = true
				}
				s.bcast.Notify(execID)
			}
			break
		}
		if _, err := writer.Append(toLogStream(frame.Stream), frame.Data); err != nil {
			appendFailed = true
			break
		}
		s.bcast.Notify(execID)
	}

	code, waitErr := session.Wait(ctx)
	state, exitCode := outcomeFor(appendFailed, code, waitErr)

	s.finish(ctx, execID, exitCode, writer.Offset(), writer.Truncated(), state)
	s.bcast.Notify(execID)
}

func outcomeFor(appendFailed bool, code int, waitErr error) (State, *int) {
	if waitErr != nil {
		return StateFailed, nil
	}
	if appendFailed {
		return StateFailed, &code
	}
	return StateExited, &code
}

func (s *Service) finish(ctx context.Context, execID string, exitCode *int, outputBytes int64, truncated bool, state State) {
	e, err := s.repo.Get(ctx, execID)
	if err != nil {
		return
	}
	finished := s.now()
	e.State = state
	e.ExitCode = exitCode
	e.OutputBytes = outputBytes
	e.Truncated = truncated
	e.FinishedAt = &finished
	s.repo.Update(ctx, e)
}

func (s *Service) Get(ctx context.Context, id string) (*Exec, error) {
	e, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.State == StateRunning {
		if info, statErr := os.Stat(s.LogPath(id)); statErr == nil {
			e.OutputBytes = info.Size()
		}
	}
	return e, nil
}

func (s *Service) ListBySandbox(ctx context.Context, sandboxID string, f Filter) ([]*Exec, error) {
	return s.repo.ListBySandbox(ctx, sandboxID, f)
}

func toLogStream(stream runtime.Stream) LogStream {
	if stream == runtime.StreamStderr {
		return LogStderr
	}
	return LogStdout
}
