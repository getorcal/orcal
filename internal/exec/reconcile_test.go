package exec_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

func seedSandbox(t *testing.T, st *sqlite.Store, id string) {
	t.Helper()
	now := time.Now().UTC()
	s := &sandbox.Sandbox{
		ID:        id,
		Image:     "alpine",
		State:     sandbox.StateRunning,
		Runtime:   "docker",
		RuntimeID: "seed-runtime-id",
		Env:       map[string]string{},
		Labels:    map[string]string{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.Sandboxes().Create(context.Background(), s); err != nil {
		t.Fatalf("seed sandbox Create() error = %v", err)
	}
}

func TestReconcileRecordsExitCodeForAnExecThatFinishedWhileDown(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "orcal.db")
	logDir := filepath.Join(dir, "execs")

	f, rid := runningFake(t, []fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("before crash")}},
	}, 9)

	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	seedSandbox(t, first, "sandbox-1")
	svc, err := exec.NewService(first.Execs(), stubLookup{runtimeID: rid}, f, logDir, 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	created, err := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"sleep"}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	svc.Wait()

	stale := &exec.Exec{
		ID:            created.ID,
		SandboxID:     created.SandboxID,
		RuntimeExecID: created.RuntimeExecID,
		Command:       created.Command,
		State:         exec.StateRunning,
		StartedAt:     created.StartedAt,
	}
	if err := first.Execs().Update(ctx, stale); err != nil {
		t.Fatalf("forcing running state error = %v", err)
	}
	first.Close()

	second, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer second.Close()
	restarted, err := exec.NewService(second.Execs(), stubLookup{runtimeID: rid}, f, logDir, 1<<20)
	if err != nil {
		t.Fatalf("NewService() after restart error = %v", err)
	}

	if err := restarted.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	restarted.Wait()

	got, err := restarted.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateExited {
		t.Errorf("State = %s, want exited", got.State)
	}
	if got.ExitCode == nil || *got.ExitCode != 9 {
		t.Errorf("ExitCode = %v, want 9", got.ExitCode)
	}
}

func TestReconcileAppendsAGapRecordForLostOutput(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	logDir := filepath.Join(dir, "execs")

	f, rid := runningFake(t, nil, 0)
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()
	seedSandbox(t, st, "sandbox-1")
	svc, err := exec.NewService(st.Execs(), stubLookup{runtimeID: rid}, f, logDir, 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	created, err := svc.Create(ctx, exec.CreateOptions{SandboxID: "sandbox-1", Command: []string{"sleep"}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	svc.Wait()

	stale := &exec.Exec{
		ID:            created.ID,
		SandboxID:     created.SandboxID,
		RuntimeExecID: created.RuntimeExecID,
		Command:       created.Command,
		State:         exec.StateRunning,
		StartedAt:     created.StartedAt,
	}
	if err := st.Execs().Update(ctx, stale); err != nil {
		t.Fatalf("forcing running state error = %v", err)
	}

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	svc.Wait()

	records, err := exec.ReadRecords(svc.LogPath(created.ID), 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	sawGap := false
	for _, record := range records {
		if record.Stream == exec.LogGap {
			sawGap = true
		}
	}
	if !sawGap {
		t.Error("no gap record written, want one marking output lost while the daemon was down")
	}
}

func TestReconcileMarksExecFailedWhenTheRuntimeHasForgottenIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	f, rid := runningFake(t, nil, 0)
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()
	seedSandbox(t, st, "sandbox-1")
	svc, err := exec.NewService(st.Execs(), stubLookup{runtimeID: rid}, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	orphan := &exec.Exec{
		ID:            "0192f3a4-5b6c-7d8e-9f01-999999999999",
		SandboxID:     "sandbox-1",
		RuntimeExecID: "vanished",
		Command:       []string{"sleep"},
		State:         exec.StateRunning,
		StartedAt:     time.Now().UTC(),
	}
	if err := st.Execs().Create(ctx, orphan); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	svc.Wait()

	got, err := svc.Get(ctx, orphan.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateFailed {
		t.Errorf("State = %s, want failed", got.State)
	}
}

func TestReconcilePollsUntilExitForAStillRunningExec(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	f, rid := runningFake(t, nil, 7)
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()
	seedSandbox(t, st, "sandbox-1")
	svc, err := exec.NewService(st.Execs(), stubLookup{runtimeID: rid}, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	sess, err := f.Exec(ctx, rid, runtime.ExecSpec{Command: []string{"sleep"}})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	stillRunning := &exec.Exec{
		ID:            "0192f3a4-5b6c-7d8e-9f01-888888888888",
		SandboxID:     "sandbox-1",
		RuntimeExecID: sess.ID(),
		Command:       []string{"sleep"},
		State:         exec.StateRunning,
		StartedAt:     time.Now().UTC(),
	}
	if err := st.Execs().Create(ctx, stillRunning); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	svc.Wait()

	got, err := svc.Get(ctx, stillRunning.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateExited {
		t.Errorf("State = %s, want exited", got.State)
	}
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("ExitCode = %v, want 7", got.ExitCode)
	}
}

func TestShutdownReturnsBeforeAStuckPollerFinishes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	f, rid := runningFake(t, nil, 0)
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()
	seedSandbox(t, st, "sandbox-1")
	svc, err := exec.NewService(st.Execs(), stubLookup{runtimeID: rid}, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	sess, err := f.Exec(ctx, rid, runtime.ExecSpec{Command: []string{"sleep"}})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	stuck := &exec.Exec{
		ID:            "0192f3a4-5b6c-7d8e-9f01-666666666666",
		SandboxID:     "sandbox-1",
		RuntimeExecID: sess.ID(),
		Command:       []string{"sleep"},
		State:         exec.StateRunning,
		StartedAt:     time.Now().UTC(),
	}
	if err := st.Execs().Create(ctx, stuck); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 300*time.Millisecond {
		t.Errorf("Shutdown() took %s, want it to return promptly on the stop signal rather than waiting for the next poll tick", elapsed)
	}

	got, err := svc.Get(ctx, stuck.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateRunning {
		t.Errorf("State = %s, want running (a process still running elsewhere must not be given a false terminal state)", got.State)
	}

	waited := make(chan struct{})
	go func() {
		svc.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after Shutdown signaled the poller to stop; the goroutine leaked")
	}
}

func TestShutdownIsSafeToCallTwice(t *testing.T) {
	f, rid := runningFake(t, nil, 0)
	svc := newService(t, stubLookup{runtimeID: rid}, f, 1<<20)
	ctx := context.Background()

	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestReconcileRecordsRealOutputBytesForAHandleGoneExec(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	f, rid := runningFake(t, nil, 0)
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()
	seedSandbox(t, st, "sandbox-1")
	svc, err := exec.NewService(st.Execs(), stubLookup{runtimeID: rid}, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	orphan := &exec.Exec{
		ID:            "0192f3a4-5b6c-7d8e-9f01-777777777777",
		SandboxID:     "sandbox-1",
		RuntimeExecID: "vanished",
		Command:       []string{"sleep"},
		State:         exec.StateRunning,
		StartedAt:     time.Now().UTC(),
	}
	if err := st.Execs().Create(ctx, orphan); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	writer, err := exec.NewLogWriter(svc.LogPath(orphan.ID), 1<<20)
	if err != nil {
		t.Fatalf("NewLogWriter() error = %v", err)
	}
	if _, err := writer.Append(exec.LogStdout, []byte("before crash")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	svc.Wait()

	got, err := svc.Get(ctx, orphan.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != exec.StateFailed {
		t.Errorf("State = %s, want failed", got.State)
	}
	if got.OutputBytes == 0 {
		t.Error("OutputBytes = 0, want the real size of the pre-crash log plus the gap record, not the stale zero from before the crash")
	}

	records, err := exec.ReadRecords(svc.LogPath(orphan.ID), 0)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	sawGap := false
	for _, record := range records {
		if record.Stream == exec.LogGap {
			sawGap = true
		}
	}
	if !sawGap {
		t.Error("no gap record written, want one marking output lost while the daemon was down")
	}
}
