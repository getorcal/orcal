package fake

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
)

func TestCreateStartInspectDestroy(t *testing.T) {
	f := New()
	ctx := context.Background()

	id, err := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerStopped {
		t.Errorf("state after create = %s, want stopped", st.State)
	}

	if err := f.Start(ctx, id); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	st, _ = f.Inspect(ctx, id)
	if st.State != runtime.ContainerRunning {
		t.Errorf("state after start = %s, want running", st.State)
	}

	if err := f.Destroy(ctx, id); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	st, _ = f.Inspect(ctx, id)
	if st.State != runtime.ContainerGone {
		t.Errorf("state after destroy = %s, want gone", st.State)
	}
}

func TestInspectUnknownReturnsGone(t *testing.T) {
	f := New()
	st, err := f.Inspect(context.Background(), "nope")
	if err != nil {
		t.Errorf("Inspect() error = %v, want nil", err)
	}
	if st.State != runtime.ContainerGone {
		t.Errorf("Inspect() state = %s, want gone", st.State)
	}
}

func TestDestroyUnknownReturnsNil(t *testing.T) {
	f := New()
	if err := f.Destroy(context.Background(), "nope"); err != nil {
		t.Errorf("Destroy() error = %v, want nil", err)
	}
}

func TestStartOnUnknownContainerReturnsErrNotFound(t *testing.T) {
	f := New()
	if err := f.Start(context.Background(), "nope"); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("Start() error = %v, want ErrNotFound", err)
	}
}

func TestStopOnUnknownContainerReturnsErrNotFound(t *testing.T) {
	f := New()
	if err := f.Stop(context.Background(), "nope", time.Second); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("Stop() error = %v, want ErrNotFound", err)
	}
}

func TestExecReplaysScriptedFramesThenExitCode(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)

	f.SetExecScript([]Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("out")}},
		{Frame: runtime.Frame{Stream: runtime.StreamStderr, Data: []byte("err")}},
	}, 3)

	sess, err := f.Exec(ctx, id, runtime.ExecSpec{Command: []string{"true"}})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	first, err := sess.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if first.Stream != runtime.StreamStdout || string(first.Data) != "out" {
		t.Errorf("first frame = %+v, want stdout out", first)
	}
	if _, err := sess.Recv(); err != nil {
		t.Fatalf("second Recv() error = %v", err)
	}
	if _, err := sess.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("third Recv() error = %v, want io.EOF", err)
	}

	code, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestInspectExecReportsCompletion(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)
	f.SetExecScript(nil, 0)

	sess, _ := f.Exec(ctx, id, runtime.ExecSpec{Command: []string{"true"}})
	status, err := f.InspectExec(ctx, sess.ID())
	if err != nil {
		t.Fatalf("InspectExec() error = %v", err)
	}
	if !status.Running {
		t.Error("Running = false before Wait, want true")
	}

	sess.Wait(ctx)
	status, _ = f.InspectExec(ctx, sess.ID())
	if status.Running {
		t.Error("Running = true after Wait, want false")
	}
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", status.ExitCode)
	}
}

func TestStopMarksContainerStopped(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)

	if err := f.Stop(ctx, id, time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerStopped {
		t.Errorf("state after stop = %s, want stopped", st.State)
	}
}

func TestLastCreateSpecReturnsMostRecentSpec(t *testing.T) {
	f := New()
	ctx := context.Background()

	first := runtime.CreateSpec{SandboxID: "sb-1", Image: "alpine"}
	if _, err := f.Create(ctx, first); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second := runtime.CreateSpec{SandboxID: "sb-2", Image: "busybox", Env: map[string]string{"FOO": "bar"}}
	if _, err := f.Create(ctx, second); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got := f.LastCreateSpec()
	if !reflect.DeepEqual(got, second) {
		t.Errorf("LastCreateSpec() = %+v, want %+v", got, second)
	}
}

func TestStartOnDestroyedContainerReturnsErrNotFound(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	if err := f.Destroy(ctx, id); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}

	if err := f.Start(ctx, id); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("Start() error = %v, want ErrNotFound", err)
	}

	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerGone {
		t.Errorf("state after failed start = %s, want gone", st.State)
	}
}

func TestDestroyIsIdempotent(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})

	if err := f.Destroy(ctx, id); err != nil {
		t.Fatalf("first Destroy() error = %v", err)
	}
	if err := f.Destroy(ctx, id); err != nil {
		t.Fatalf("second Destroy() error = %v", err)
	}

	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerGone {
		t.Errorf("state after double destroy = %s, want gone", st.State)
	}
}

func TestExecOnUnstartedContainerReturnsErrConflict(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})

	_, err := f.Exec(ctx, id, runtime.ExecSpec{Command: []string{"true"}})
	if !errors.Is(err, runtime.ErrConflict) {
		t.Errorf("Exec() error = %v, want ErrConflict", err)
	}
}

func TestLastCreateSpecUnaffectedByFailedCreate(t *testing.T) {
	f := New()
	ctx := context.Background()

	first := runtime.CreateSpec{SandboxID: "sb-1", Image: "alpine"}
	if _, err := f.Create(ctx, first); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	f.SetCreateError(errors.New("boom"))
	if _, err := f.Create(ctx, runtime.CreateSpec{SandboxID: "sb-2", Image: "busybox"}); err == nil {
		t.Fatal("second Create() error = nil, want error")
	}

	got := f.LastCreateSpec()
	if !reflect.DeepEqual(got, first) {
		t.Errorf("LastCreateSpec() = %+v, want %+v", got, first)
	}
}

func TestSnapshotReturnsRefAndSize(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)

	info, err := f.Snapshot(ctx, id)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if info.Ref == "" {
		t.Error("Ref is empty")
	}
	if info.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want positive", info.SizeBytes)
	}
}

func TestSnapshotLeavesContainerRunning(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)

	f.Snapshot(ctx, id)

	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerRunning {
		t.Errorf("state after snapshot = %s, want running", st.State)
	}
}

func TestSnapshotLeavesContainerUnpausedWhenItFails(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)
	f.SetSnapshotError(errors.New("commit exploded"))

	if _, err := f.Snapshot(ctx, id); err == nil {
		t.Fatal("Snapshot() error = nil, want the injected failure")
	}

	st, _ := f.Inspect(ctx, id)
	if st.State == runtime.ContainerPaused {
		t.Error("container left paused after a failed snapshot")
	}
	if st.State != runtime.ContainerRunning {
		t.Errorf("state = %s, want running", st.State)
	}
}

func TestSnapshotOfAStoppedContainerSucceeds(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})

	if _, err := f.Snapshot(ctx, id); err != nil {
		t.Fatalf("Snapshot() on stopped container error = %v", err)
	}
	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerStopped {
		t.Errorf("state = %s, want stopped", st.State)
	}
}

func TestSnapshotOfUnknownContainerReturnsErrNotFound(t *testing.T) {
	f := New()
	if _, err := f.Snapshot(context.Background(), "nope"); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("Snapshot() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteSnapshotRefusesWhileAContainerUsesIt(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)
	info, _ := f.Snapshot(ctx, id)

	f.Create(ctx, runtime.CreateSpec{Image: info.Ref})

	if err := f.DeleteSnapshot(ctx, info.Ref); !errors.Is(err, runtime.ErrConflict) {
		t.Errorf("DeleteSnapshot() error = %v, want ErrConflict", err)
	}
}

func TestDeleteSnapshotSucceedsWhenUnused(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)
	info, _ := f.Snapshot(ctx, id)

	if err := f.DeleteSnapshot(ctx, info.Ref); err != nil {
		t.Errorf("DeleteSnapshot() error = %v, want nil", err)
	}
}

func TestUnpauseRestoresARunningContainer(t *testing.T) {
	f := New()
	ctx := context.Background()
	id, _ := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	f.Start(ctx, id)
	f.ForcePaused(id)

	st, _ := f.Inspect(ctx, id)
	if st.State != runtime.ContainerPaused {
		t.Fatalf("state = %s, want paused", st.State)
	}
	if err := f.Unpause(ctx, id); err != nil {
		t.Fatalf("Unpause() error = %v", err)
	}
	st, _ = f.Inspect(ctx, id)
	if st.State != runtime.ContainerRunning {
		t.Errorf("state after unpause = %s, want running", st.State)
	}
}

var _ runtime.Runtime = (*Fake)(nil)
