package sandbox_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

type hookRepo struct {
	sandbox.Repo
	onGet func(ctx context.Context, id string)
}

func (r *hookRepo) Get(ctx context.Context, id string) (*sandbox.Sandbox, error) {
	if r.onGet != nil {
		r.onGet(ctx, id)
	}
	return r.Repo.Get(ctx, id)
}

type stubSnapshots struct {
	ref     string
	id      string
	network string
	err     error
}

func (s stubSnapshots) Resolve(ctx context.Context, ref string) (snapshot.Resolved, error) {
	return snapshot.Resolved{RuntimeRef: s.ref, ID: s.id, Network: s.network}, s.err
}

func TestForkCreatesANewSandboxFromTheSnapshotRef(t *testing.T) {
	svc, f := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()

	forked, err := svc.Fork(ctx, "working-v1", sandbox.CreateOptions{Name: "experiment-a"})
	if err != nil {
		t.Fatalf("Fork() error = %v", err)
	}
	if forked.State != sandbox.StateRunning {
		t.Errorf("State = %s, want running", forked.State)
	}
	if forked.ParentSnapshotID == nil || *forked.ParentSnapshotID != "sn-1" {
		t.Errorf("ParentSnapshotID = %v, want sn-1", forked.ParentSnapshotID)
	}
	if spec := f.LastCreateSpec(); spec.Image != "sha256:snap" {
		t.Errorf("spec.Image = %q, want the snapshot ref", spec.Image)
	}
}

func TestForkAppliesResourceDefaultsRatherThanInheriting(t *testing.T) {
	svc, f := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})

	forked, err := svc.Fork(context.Background(), "working-v1", sandbox.CreateOptions{Name: "experiment-a"})
	if err != nil {
		t.Fatalf("Fork() error = %v", err)
	}
	if forked.Resources.CPUMillis != 1000 || forked.Resources.PidsLimit != 512 {
		t.Errorf("Resources = %+v, want the configured defaults", forked.Resources)
	}
	if spec := f.LastCreateSpec(); spec.CPUMillis != 1000 {
		t.Errorf("spec.CPUMillis = %d, want 1000", spec.CPUMillis)
	}
}

func TestForkHonoursExplicitResources(t *testing.T) {
	svc, _ := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})

	forked, _ := svc.Fork(context.Background(), "working-v1", sandbox.CreateOptions{
		Name:      "experiment-a",
		Resources: sandbox.Resources{CPUMillis: 4000, MemoryBytes: 8 << 30, PidsLimit: 64},
	})
	if forked.Resources.CPUMillis != 4000 || forked.Resources.PidsLimit != 64 {
		t.Errorf("Resources = %+v, want the explicit values", forked.Resources)
	}
}

func TestForkPropagatesAnUnknownSnapshot(t *testing.T) {
	svc, _ := newService(t)
	svc.SetSnapshots(stubSnapshots{err: snapshot.ErrNotFound})

	_, err := svc.Fork(context.Background(), "ghost", sandbox.CreateOptions{})
	if !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Fork() error = %v, want snapshot.ErrNotFound", err)
	}
}

func TestForkDoesNotClobberAConcurrentChangeToTheForkedSandbox(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	base := st.Sandboxes()
	fired := false
	repo := &hookRepo{Repo: base}
	repo.onGet = func(ctx context.Context, id string) {
		if fired {
			return
		}
		fired = true
		sb, err := base.Get(ctx, id)
		if err != nil {
			t.Fatalf("onGet: base.Get() error = %v", err)
		}
		sb.State = sandbox.StateDestroyed
		sb.UpdatedAt = time.Now().UTC()
		if err := base.Update(ctx, sb); err != nil {
			t.Fatalf("onGet: base.Update() error = %v", err)
		}
	}

	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	svc := sandbox.NewService(repo, fake.New(), defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"}, "")
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()

	forked, err := svc.Fork(ctx, "working-v1", sandbox.CreateOptions{Name: "experiment-a"})
	if err != nil {
		t.Fatalf("Fork() error = %v", err)
	}

	got, err := base.Get(ctx, forked.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != sandbox.StateDestroyed {
		t.Errorf("State = %s, want destroyed - Fork must not clobber a concurrent change", got.State)
	}
	if got.ParentSnapshotID == nil || *got.ParentSnapshotID != "sn-1" {
		t.Errorf("ParentSnapshotID = %v, want sn-1", got.ParentSnapshotID)
	}
}

func TestRestoreReplacesTheContainerAndKeepsIdentity(t *testing.T) {
	svc, f := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()

	original, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	oldRuntimeID := original.RuntimeID

	restored, err := svc.Restore(ctx, "my-agent", "working-v1")
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.ID != original.ID || restored.Name != "my-agent" {
		t.Errorf("identity changed: %s/%s, want %s/my-agent", restored.ID, restored.Name, original.ID)
	}
	if restored.RuntimeID == oldRuntimeID {
		t.Error("RuntimeID unchanged; the container was not replaced")
	}
	if restored.State != sandbox.StateRunning {
		t.Errorf("State = %s, want running", restored.State)
	}
	if restored.ParentSnapshotID == nil || *restored.ParentSnapshotID != "sn-1" {
		t.Errorf("ParentSnapshotID = %v, want sn-1", restored.ParentSnapshotID)
	}
	if st, _ := f.Inspect(ctx, oldRuntimeID); st.State != runtime.ContainerGone {
		t.Errorf("old container state = %s, want gone", st.State)
	}
}

func TestRestorePreservesTheNetworkMode(t *testing.T) {
	svc, f := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20", Network: sandbox.NetworkNone})

	restored, err := svc.Restore(ctx, "my-agent", "working-v1")
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.Network != sandbox.NetworkNone {
		t.Errorf("Network = %q, want none", restored.Network)
	}
	if got := f.LastCreateSpec().NetworkName; got != "orcal-isolated" {
		t.Errorf("spec.NetworkName = %q, want orcal-isolated - a restored none sandbox must not regain internet access", got)
	}
}

func TestRestoreReportsTheRuntimeItIsActuallyRunningUnderRatherThanTheOneItHadBefore(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	repo := st.Sandboxes()

	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	svc := sandbox.NewService(repo, fake.New(), defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"}, "runsc")
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()

	original, err := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	original.OCIRuntime = "stale-runtime"
	if err := repo.Update(ctx, original); err != nil {
		t.Fatalf("seed stale OCIRuntime: %v", err)
	}

	restored, err := svc.Restore(ctx, "my-agent", "working-v1")
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.OCIRuntime != "runsc" {
		t.Errorf("restored.OCIRuntime = %q, want runsc (the runtime actually in use), not the stale value it had before", restored.OCIRuntime)
	}
}

func TestRestoreLeavesTheSandboxErroredWhenTheRuntimeFails(t *testing.T) {
	svc, f := newService(t)
	svc.SetSnapshots(stubSnapshots{ref: "sha256:snap", id: "sn-1"})
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	f.SetCreateError(runtime.ErrUnavailable)
	if _, err := svc.Restore(ctx, "my-agent", "working-v1"); err == nil {
		t.Fatal("Restore() error = nil, want the runtime failure")
	}

	got, err := svc.Get(ctx, "my-agent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != sandbox.StateError {
		t.Errorf("State = %s, want error", got.State)
	}
}

func TestWithSnapshotSourceExposesLineageUnderTheLock(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	var seen snapshot.SandboxSource
	err := svc.WithSnapshotSource(ctx, "my-agent", func(src snapshot.SandboxSource) error {
		seen = src
		return nil
	})
	if err != nil {
		t.Fatalf("WithSnapshotSource() error = %v", err)
	}
	if seen.ID != created.ID || seen.RuntimeID != created.RuntimeID || seen.Image != "alpine:3.20" {
		t.Errorf("source = %+v, want the created sandbox's identity", seen)
	}
	if seen.ParentSnapshotID != nil {
		t.Errorf("ParentSnapshotID = %v, want nil", seen.ParentSnapshotID)
	}
}

func TestWithSnapshotSourceRefusesADestroyedSandbox(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	svc.Destroy(ctx, "my-agent")

	err := svc.WithSnapshotSource(ctx, "my-agent", func(snapshot.SandboxSource) error { return nil })
	if !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("WithSnapshotSource() on a destroyed sandbox = %v, want ErrNotFound", err)
	}
}

func TestWithSnapshotSourceRefusesASandboxInError(t *testing.T) {
	svc := newServiceWithRuntime(t, &erroringRuntime{Fake: fake.New(), startErr: errors.New("start refused")})
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	err := svc.WithSnapshotSource(ctx, "my-agent", func(snapshot.SandboxSource) error { return nil })
	if !errors.Is(err, sandbox.ErrInvalidState) {
		t.Errorf("WithSnapshotSource() on an errored sandbox = %v, want ErrInvalidState", err)
	}
}

func TestUnpausePausedClearsAFrozenContainer(t *testing.T) {
	svc, f := newService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	f.ForcePaused(created.RuntimeID)

	n, err := svc.UnpausePaused(ctx)
	if err != nil {
		t.Fatalf("UnpausePaused() error = %v", err)
	}
	if n != 1 {
		t.Errorf("unpaused = %d, want 1", n)
	}
	if st, _ := f.Inspect(ctx, created.RuntimeID); st.State != runtime.ContainerRunning {
		t.Errorf("state = %s, want running", st.State)
	}
}

func TestUnpausePausedIgnoresHealthySandboxes(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	n, err := svc.UnpausePaused(ctx)
	if err != nil {
		t.Fatalf("UnpausePaused() error = %v", err)
	}
	if n != 0 {
		t.Errorf("unpaused = %d, want 0", n)
	}
}
