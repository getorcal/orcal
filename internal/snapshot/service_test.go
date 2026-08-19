package snapshot_test

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

type stubAccess struct {
	src snapshot.SandboxSource
	err error
}

func (s stubAccess) WithSnapshotSource(ctx context.Context, ref string, fn func(snapshot.SandboxSource) error) error {
	if s.err != nil {
		return s.err
	}
	return fn(s.src)
}

func newService(t *testing.T, access snapshot.SandboxAccess, f *fake.Fake) (*snapshot.Service, *sqlite.Store) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return snapshot.NewService(st.Snapshots(), access, f), st
}

func seedSandbox(t *testing.T, st *sqlite.Store, id string) {
	t.Helper()
	now := timeNow()
	err := st.Sandboxes().Create(context.Background(), &sandbox.Sandbox{
		ID: id, Image: "alpine:3.20", State: sandbox.StateRunning,
		Runtime: "docker", RuntimeID: "c-" + id,
		Resources: sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512},
		Env:       map[string]string{}, Labels: map[string]string{},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed sandbox: %v", err)
	}
}

func runningFake(t *testing.T) (*fake.Fake, string) {
	t.Helper()
	f := fake.New()
	id, err := f.Create(context.Background(), runtime.CreateSpec{Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("fake Create() error = %v", err)
	}
	f.Start(context.Background(), id)
	return f, id
}

func TestCreateRecordsRuntimeRefAndSize(t *testing.T) {
	f, rid := runningFake(t)
	access := stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid, Image: "alpine:3.20"}}
	svc, st := newService(t, access, f)
	seedSandbox(t, st, "sb-1")

	got, err := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1", Name: "working-v1"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.RuntimeRef == "" {
		t.Error("RuntimeRef is empty")
	}
	if got.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want positive", got.SizeBytes)
	}
	if got.SandboxID != "sb-1" || got.Image != "alpine:3.20" {
		t.Errorf("origin = %q/%q, want sb-1/alpine:3.20", got.SandboxID, got.Image)
	}
	if got.ParentID != nil {
		t.Errorf("ParentID = %v, want nil for a sandbox created from a base image", got.ParentID)
	}
}

func TestCreateInheritsParentFromTheSandboxLineage(t *testing.T) {
	f, rid := runningFake(t)
	parent := "sn-parent"
	access := stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid, Image: "alpine:3.20", ParentSnapshotID: &parent}}
	svc, st := newService(t, access, f)
	seedSandbox(t, st, "sb-1")
	st.Snapshots().Create(context.Background(), &snapshot.Snapshot{
		ID: parent, SandboxID: "sb-1", RuntimeRef: "sha256:p", Image: "alpine:3.20", CreatedAt: timeNow(),
	})

	got, err := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.ParentID == nil || *got.ParentID != parent {
		t.Errorf("ParentID = %v, want %q", got.ParentID, parent)
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")

	_, err := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1", Name: "Bad_Name"})
	if !errors.Is(err, snapshot.ErrInvalidName) {
		t.Errorf("Create() error = %v, want ErrInvalidName", err)
	}
}

func TestCreatePropagatesTheSandboxLookupError(t *testing.T) {
	f, _ := runningFake(t)
	boom := errors.New("sandbox is stopped")
	svc, _ := newService(t, stubAccess{err: boom}, f)

	if _, err := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1"}); !errors.Is(err, boom) {
		t.Errorf("Create() error = %v, want the lookup error", err)
	}
}

func TestCreateDoesNotPersistWhenTheRuntimeFails(t *testing.T) {
	f, rid := runningFake(t)
	f.SetSnapshotError(errors.New("commit exploded"))
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")

	if _, err := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1"}); err == nil {
		t.Fatal("Create() error = nil, want the runtime failure")
	}

	all, _ := svc.List(context.Background(), snapshot.Filter{Limit: 10})
	if len(all) != 0 {
		t.Errorf("persisted %d snapshots after a runtime failure, want 0", len(all))
	}
}

func TestGetResolvesByIDAndName(t *testing.T) {
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")
	created, _ := svc.Create(context.Background(), snapshot.CreateOptions{SandboxRef: "sb-1", Name: "working-v1"})

	byID, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(id) error = %v", err)
	}
	byName, err := svc.Get(context.Background(), "working-v1")
	if err != nil {
		t.Fatalf("Get(name) error = %v", err)
	}
	if byID.ID != byName.ID {
		t.Errorf("Get(id) = %s, Get(name) = %s, want equal", byID.ID, byName.ID)
	}
}

func TestDeleteRefusesWhileDescendantsExist(t *testing.T) {
	ctx := context.Background()
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")

	parent, _ := svc.Create(ctx, snapshot.CreateOptions{SandboxRef: "sb-1", Name: "v1"})
	st.Snapshots().Create(ctx, &snapshot.Snapshot{
		ID: "sn-child", Name: "v2", SandboxID: "sb-1", ParentID: &parent.ID,
		RuntimeRef: "sha256:child", Image: "alpine:3.20", CreatedAt: timeNow(),
	})

	err := svc.Delete(ctx, parent.ID)
	if !errors.Is(err, snapshot.ErrHasChildren) {
		t.Fatalf("Delete() error = %v, want ErrHasChildren", err)
	}

	if _, err := svc.Get(ctx, parent.ID); err != nil {
		t.Errorf("snapshot was removed despite the refusal: %v", err)
	}
}

func TestDeleteRemovesImageBeforeRow(t *testing.T) {
	ctx := context.Background()
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")
	created, _ := svc.Create(ctx, snapshot.CreateOptions{SandboxRef: "sb-1", Name: "v1"})

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Get() after delete = %v, want ErrNotFound", err)
	}
	if err := f.DeleteSnapshot(ctx, created.RuntimeRef); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("image still present after delete: %v", err)
	}
}

func TestDeleteKeepsTheRowWhenTheRuntimeRefuses(t *testing.T) {
	ctx := context.Background()
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")
	created, _ := svc.Create(ctx, snapshot.CreateOptions{SandboxRef: "sb-1", Name: "v1"})

	f.Create(ctx, runtime.CreateSpec{Image: created.RuntimeRef})

	if err := svc.Delete(ctx, created.ID); !errors.Is(err, runtime.ErrConflict) {
		t.Fatalf("Delete() error = %v, want ErrConflict", err)
	}
	if _, err := svc.Get(ctx, created.ID); err != nil {
		t.Errorf("row deleted despite the image removal failing: %v", err)
	}
}

func TestResolveReturnsRuntimeRefAndID(t *testing.T) {
	ctx := context.Background()
	f, rid := runningFake(t)
	svc, st := newService(t, stubAccess{src: snapshot.SandboxSource{ID: "sb-1", RuntimeID: rid}}, f)
	seedSandbox(t, st, "sb-1")
	created, _ := svc.Create(ctx, snapshot.CreateOptions{SandboxRef: "sb-1", Name: "v1"})

	resolved, err := svc.Resolve(ctx, "v1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.RuntimeRef != created.RuntimeRef || resolved.ID != created.ID {
		t.Errorf("Resolve() = %q/%q, want %q/%q", resolved.RuntimeRef, resolved.ID, created.RuntimeRef, created.ID)
	}
}

func timeNow() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
