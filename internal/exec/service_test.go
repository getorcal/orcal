package exec_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

type stubLookup struct {
	runtimeID string
	err       error
}

func (s stubLookup) RuntimeID(ctx context.Context, sandboxID string) (string, error) {
	return s.runtimeID, s.err
}

type wrapRuntime struct {
	runtime.Runtime
	wrap func(runtime.Session) runtime.Session
}

func (w wrapRuntime) Exec(ctx context.Context, runtimeID string, spec runtime.ExecSpec) (runtime.Session, error) {
	sess, err := w.Runtime.Exec(ctx, runtimeID, spec)
	if err != nil {
		return nil, err
	}
	return w.wrap(sess), nil
}

type blockingSession struct {
	runtime.Session
	release chan struct{}
	once    sync.Once
}

func (s *blockingSession) Recv() (runtime.Frame, error) {
	s.once.Do(func() { <-s.release })
	return s.Session.Recv()
}

type failingWaitSession struct {
	runtime.Session
	err error
}

func (s *failingWaitSession) Wait(ctx context.Context) (int, error) {
	return 0, s.err
}

type failingUpdateRepo struct {
	exec.Repo
	failOn int
	calls  int
}

func (r *failingUpdateRepo) Update(ctx context.Context, e *exec.Exec) error {
	r.calls++
	if r.calls == r.failOn {
		return errors.New("simulated update failure")
	}
	return r.Repo.Update(ctx, e)
}

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.Now().UTC()
	seed := &sandbox.Sandbox{
		ID:        "sandbox-1",
		Image:     "alpine",
		State:     sandbox.StateRunning,
		Runtime:   "docker",
		RuntimeID: "seed-runtime-id",
		Env:       map[string]string{},
		Labels:    map[string]string{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.Sandboxes().Create(context.Background(), seed); err != nil {
		t.Fatalf("seed sandbox Create() error = %v", err)
	}
	return st
}

func newService(t *testing.T, lookup exec.SandboxLookup, rt runtime.Runtime, maxBytes int64) *exec.Service {
	t.Helper()
	st := newStore(t)
	svc, err := exec.NewService(st.Execs(), lookup, rt, t.TempDir(), maxBytes)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func runningFake(t *testing.T, script []fake.Step, exitCode int) (*fake.Fake, string) {
	t.Helper()
	f := fake.New()
	id, err := f.Create(context.Background(), runtime.CreateSpec{Image: "alpine"})
	if err != nil {
		t.Fatalf("fake Create() error = %v", err)
	}
	f.Start(context.Background(), id)
	f.SetExecScript(script, exitCode)
	return f, id
}

func TestCreateReturnsRunningExecImmediately(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)

	e, err := svc.Create(context.Background(), exec.CreateOptions{
		SandboxID: "sandbox-1",
		Command:   []string{"true"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if e.ID == "" {
		t.Error("ID is empty")
	}
	if e.State != exec.StateRunning {
		t.Errorf("State = %s, want running", e.State)
	}
	if e.RuntimeExecID == "" {
		t.Error("RuntimeExecID is empty, want the runtime handle recorded for restart reconciliation")
	}
}

func TestSupervisorPersistsOutputAndExitCode(t *testing.T) {
	script := []fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("hello ")}},
		{Frame: runtime.Frame{Stream: runtime.StreamStderr, Data: []byte("world")}},
	}
	f, rid := runningFake(t, script, 42)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"echo"}})
	svc.Wait()

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateExited {
		t.Errorf("State = %s, want exited", got.State)
	}
	if got.ExitCode == nil || *got.ExitCode != 42 {
		t.Errorf("ExitCode = %v, want 42", got.ExitCode)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt is nil, want a completion timestamp")
	}

	records, err := exec.ReadRecords(svc.LogPath(created.ID), 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Stream != exec.LogStdout || string(records[0].Data) != "hello " {
		t.Errorf("records[0] = %+v, want stdout 'hello '", records[0])
	}
	if records[1].Stream != exec.LogStderr || string(records[1].Data) != "world" {
		t.Errorf("records[1] = %+v, want stderr 'world'", records[1])
	}
	if got.OutputBytes != records[1].Offset {
		t.Errorf("OutputBytes = %d, want %d", got.OutputBytes, records[1].Offset)
	}
}

func TestGetReportsLiveOutputBytesWhileRunning(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"true"}})
	svc.Wait()

	got, _ := svc.Get(ctx, created.ID)
	if got.OutputBytes != 0 {
		t.Errorf("OutputBytes = %d, want 0 for an exec that produced no output", got.OutputBytes)
	}
}

func TestTruncationIsRecordedWhenOutputExceedsCap(t *testing.T) {
	script := []fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("12345")}},
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("67890")}},
	}
	f, rid := runningFake(t, script, 0)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 12)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"yes"}})
	svc.Wait()

	got, _ := svc.Get(ctx, created.ID)
	if !got.Truncated {
		t.Error("Truncated = false, want true once the cap is hit")
	}
	if got.State != exec.StateExited {
		t.Errorf("State = %s, want exited (truncation is a recorded condition, not a failure)", got.State)
	}
	records, _ := exec.ReadRecords(svc.LogPath(created.ID), 0)
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1 (second frame dropped)", len(records))
	}
}

func TestCreateFailsWhenSandboxNotRunning(t *testing.T) {
	f, _ := runningFake(t, nil, 0)
	lookupErr := errors.New("sandbox is stopped")
	svc := newService(t, stubLookup{err: lookupErr}, f, 1<<20)

	_, err := svc.Create(context.Background(), exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"true"}})
	if !errors.Is(err, lookupErr) {
		t.Errorf("Create() error = %v, want the lookup error", err)
	}
}

func TestCreateMarksExecFailedWhenRuntimeExecFails(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	f.SetExecError(runtime.ErrUnavailable)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)
	ctx := context.Background()

	_, err := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"true"}})
	if err == nil {
		t.Fatal("Create() error = nil, want runtime failure")
	}

	all, listErr := svc.ListBySandbox(ctx, "sandbox-1", exec.Filter{Limit: 10})
	if listErr != nil {
		t.Fatalf("ListBySandbox() error = %v", listErr)
	}
	if len(all) != 1 {
		t.Fatalf("len(execs) = %d, want 1 failed record", len(all))
	}
	if all[0].State != exec.StateFailed {
		t.Errorf("State = %s, want failed", all[0].State)
	}
}

func TestCreateMarksExecFailedWhenRuntimeExecIDUpdateFails(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	st := newStore(t)
	repo := &failingUpdateRepo{Repo: st.Execs(), failOn: 1}
	svc, err := exec.NewService(repo, stubLookup{runtimeID: rid}, f, t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx := context.Background()

	_, err = svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"true"}})
	if err == nil {
		t.Fatal("Create() error = nil, want the repo update failure")
	}

	all, listErr := svc.ListBySandbox(ctx, "sandbox-1", exec.Filter{Limit: 10})
	if listErr != nil {
		t.Fatalf("ListBySandbox() error = %v", listErr)
	}
	if len(all) != 1 {
		t.Fatalf("len(execs) = %d, want 1", len(all))
	}
	if all[0].State != exec.StateFailed {
		t.Errorf("State = %s, want failed (RuntimeExecID update failed, exec must not be stranded running)", all[0].State)
	}
}

func TestBroadcasterWakesWaiterOnNotify(t *testing.T) {
	b := exec.NewBroadcaster()
	ch := b.Wait("exec-1")

	select {
	case <-ch:
		t.Fatal("channel closed before Notify")
	default:
	}

	b.Notify("exec-1")
	select {
	case <-ch:
	default:
		t.Fatal("channel not closed after Notify")
	}
}

func TestBroadcasterIssuesFreshChannelAfterNotify(t *testing.T) {
	b := exec.NewBroadcaster()
	first := b.Wait("exec-1")
	b.Notify("exec-1")
	<-first

	second := b.Wait("exec-1")
	select {
	case <-second:
		t.Fatal("second channel already closed, want a fresh one")
	default:
	}
}

func TestSupervisorNotifiesReadersOnCompletion(t *testing.T) {
	f, rid := runningFake(t, []fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("x")}},
	}, 0)
	release := make(chan struct{})
	rt := wrapRuntime{Runtime: f, wrap: func(sess runtime.Session) runtime.Session {
		return &blockingSession{Session: sess, release: release}
	}}
	svc := newService(t, stubLookup{runtimeID: rid}, rt, 1<<20)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"echo"}})
	ch := svc.Broadcaster().Wait(created.ID)
	close(release)
	svc.Wait()

	select {
	case <-ch:
	default:
		t.Error("broadcaster did not notify after the exec completed")
	}
}

func TestSupervisorMarksExecFailedWhenAppendFails(t *testing.T) {
	script := []fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: bytes.Repeat([]byte("x"), exec.MaxFramePayload+1)}},
	}
	f, rid := runningFake(t, script, 0)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"echo"}})
	svc.Wait()

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateFailed {
		t.Errorf("State = %s, want failed when a frame could not be persisted, even though the process itself exited cleanly", got.State)
	}
	if got.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil for a failed exec", *got.ExitCode)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt is nil, want a completion timestamp")
	}
}

func TestSupervisorMarksExecFailedWhenWaitErrors(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	waitErr := errors.New("wait boom")
	rt := wrapRuntime{Runtime: f, wrap: func(sess runtime.Session) runtime.Session {
		return &failingWaitSession{Session: sess, err: waitErr}
	}}
	svc := newService(t, stubLookup{runtimeID: rid}, rt, 1<<20)
	ctx := context.Background()

	created, _ := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"true"}})
	svc.Wait()

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateFailed {
		t.Errorf("State = %s, want failed when Wait returns an error", got.State)
	}
	if got.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil", *got.ExitCode)
	}
}
