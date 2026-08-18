package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

func seedSandbox(t *testing.T, st *Store, id string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	err := st.Sandboxes().Create(context.Background(), &sandbox.Sandbox{
		ID:        id,
		Image:     "alpine:3.20",
		State:     sandbox.StateRunning,
		Runtime:   "docker",
		RuntimeID: "container-" + id,
		Resources: sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512},
		Env:       map[string]string{},
		Labels:    map[string]string{},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed sandbox: %v", err)
	}
}

func sampleSnapshot(id, sandboxID string) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		ID:         id,
		Name:       "working-v1",
		SandboxID:  sandboxID,
		RuntimeRef: "sha256:abc",
		Image:      "alpine:3.20",
		SizeBytes:  4096,
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestSnapshotRoundTripPreservesAllFields(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()

	want := sampleSnapshot("sn-1", "sb-1")
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, "sn-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != want.Name || got.SandboxID != want.SandboxID {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
	if got.RuntimeRef != "sha256:abc" || got.Image != "alpine:3.20" || got.SizeBytes != 4096 {
		t.Errorf("payload = %q/%q/%d, want sha256:abc/alpine:3.20/4096", got.RuntimeRef, got.Image, got.SizeBytes)
	}
	if got.ParentID != nil {
		t.Errorf("ParentID = %v, want nil", got.ParentID)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestSnapshotParentRoundTrips(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()

	parent := sampleSnapshot("sn-1", "sb-1")
	repo.Create(ctx, parent)

	child := sampleSnapshot("sn-2", "sb-1")
	child.Name = "working-v2"
	child.ParentID = &parent.ID
	if err := repo.Create(ctx, child); err != nil {
		t.Fatalf("Create(child) error = %v", err)
	}

	got, _ := repo.Get(ctx, "sn-2")
	if got.ParentID == nil || *got.ParentID != "sn-1" {
		t.Errorf("ParentID = %v, want sn-1", got.ParentID)
	}
}

func TestGetMissingSnapshotReturnsErrNotFound(t *testing.T) {
	_, err := newStore(t).Snapshots().Get(context.Background(), "missing")
	if !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Get() error = %v, want snapshot.ErrNotFound", err)
	}
}

func TestGetSnapshotByName(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	st.Snapshots().Create(ctx, sampleSnapshot("sn-1", "sb-1"))

	got, err := st.Snapshots().GetByName(ctx, "working-v1")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if got.ID != "sn-1" {
		t.Errorf("ID = %q, want sn-1", got.ID)
	}
}

func TestDuplicateSnapshotNameReturnsErrNameTaken(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()
	repo.Create(ctx, sampleSnapshot("sn-1", "sb-1"))

	second := sampleSnapshot("sn-2", "sb-1")
	if err := repo.Create(ctx, second); !errors.Is(err, snapshot.ErrNameTaken) {
		t.Errorf("Create() error = %v, want snapshot.ErrNameTaken", err)
	}
}

func TestMultipleUnnamedSnapshotsCoexist(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()

	a := sampleSnapshot("sn-1", "sb-1")
	a.Name = ""
	b := sampleSnapshot("sn-2", "sb-1")
	b.Name = ""
	repo.Create(ctx, a)
	if err := repo.Create(ctx, b); err != nil {
		t.Errorf("second unnamed Create() error = %v, want nil", err)
	}
}

func TestCountChildren(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()

	parent := sampleSnapshot("sn-1", "sb-1")
	repo.Create(ctx, parent)

	n, err := repo.CountChildren(ctx, "sn-1")
	if err != nil {
		t.Fatalf("CountChildren() error = %v", err)
	}
	if n != 0 {
		t.Errorf("children = %d, want 0", n)
	}

	child := sampleSnapshot("sn-2", "sb-1")
	child.Name = "working-v2"
	child.ParentID = &parent.ID
	repo.Create(ctx, child)

	n, _ = repo.CountChildren(ctx, "sn-1")
	if n != 1 {
		t.Errorf("children after fork = %d, want 1", n)
	}
}

func TestListFiltersBySandboxAndPaginates(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	seedSandbox(t, st, "sb-2")
	repo := st.Snapshots()

	for i, id := range []string{"sn-1", "sn-2", "sn-3"} {
		s := sampleSnapshot(id, "sb-1")
		s.Name = "s" + string(rune('a'+i))
		repo.Create(ctx, s)
	}
	other := sampleSnapshot("sn-9", "sb-2")
	other.Name = "other"
	repo.Create(ctx, other)

	mine, err := repo.List(ctx, snapshot.Filter{SandboxID: "sb-1", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(mine) != 3 {
		t.Fatalf("len = %d, want 3", len(mine))
	}

	page, _ := repo.List(ctx, snapshot.Filter{SandboxID: "sb-1", Limit: 2})
	if len(page) != 2 {
		t.Fatalf("page len = %d, want 2", len(page))
	}
	rest, _ := repo.List(ctx, snapshot.Filter{SandboxID: "sb-1", Limit: 2, Cursor: page[1].ID})
	if len(rest) != 1 {
		t.Errorf("second page len = %d, want 1", len(rest))
	}
}

func TestDeleteSnapshotRow(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()
	repo.Create(ctx, sampleSnapshot("sn-1", "sb-1"))

	if err := repo.Delete(ctx, "sn-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.Get(ctx, "sn-1"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Get() after delete = %v, want ErrNotFound", err)
	}
}

func TestDeletedSnapshotFreesItsName(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	repo := st.Snapshots()
	repo.Create(ctx, sampleSnapshot("sn-1", "sb-1"))
	repo.Delete(ctx, "sn-1")

	if err := repo.Create(ctx, sampleSnapshot("sn-2", "sb-1")); err != nil {
		t.Errorf("Create() reusing freed name error = %v, want nil", err)
	}
}

func TestSandboxParentSnapshotRoundTrips(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedSandbox(t, st, "sb-1")
	st.Snapshots().Create(ctx, sampleSnapshot("sn-1", "sb-1"))

	sb, _ := st.Sandboxes().Get(ctx, "sb-1")
	if sb.ParentSnapshotID != nil {
		t.Errorf("ParentSnapshotID = %v, want nil initially", sb.ParentSnapshotID)
	}

	id := "sn-1"
	sb.ParentSnapshotID = &id
	if err := st.Sandboxes().Update(ctx, sb); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, _ := st.Sandboxes().Get(ctx, "sb-1")
	if got.ParentSnapshotID == nil || *got.ParentSnapshotID != "sn-1" {
		t.Errorf("ParentSnapshotID = %v, want sn-1", got.ParentSnapshotID)
	}
}
