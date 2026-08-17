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

func TestInspectUnknownReturnsErrNotFound(t *testing.T) {
	f := New()
	_, err := f.Inspect(context.Background(), "nope")
	if !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("Inspect() error = %v, want ErrNotFound", err)
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

var _ runtime.Runtime = (*Fake)(nil)
