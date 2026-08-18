package orcalclient_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestClientSnapshotRoundTrip(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})

	snap, err := c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "working-v1"})
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if snap.Id == "" {
		t.Fatal("id is empty")
	}

	got, err := c.GetSnapshot(ctx, "working-v1")
	if err != nil {
		t.Fatalf("GetSnapshot() error = %v", err)
	}
	if got.Id != snap.Id {
		t.Errorf("id = %q, want %q", got.Id, snap.Id)
	}
}

func TestClientForkViaCreateWithSnapshot(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	snap, _ := c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "v1"})

	forked, err := c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "experiment-a", Snapshot: "v1"})
	if err != nil {
		t.Fatalf("CreateSandbox(from snapshot) error = %v", err)
	}
	if forked.ParentSnapshotId == nil || *forked.ParentSnapshotId != snap.Id {
		t.Errorf("parent = %v, want %q", forked.ParentSnapshotId, snap.Id)
	}
}

func TestClientRestore(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "v1"})

	restored, err := c.RestoreSandbox(ctx, "my-agent", "v1")
	if err != nil {
		t.Fatalf("RestoreSandbox() error = %v", err)
	}
	if restored.State != "running" {
		t.Errorf("state = %q, want running", restored.State)
	}
}

func TestClientDeleteSnapshot(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	snap, _ := c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "v1"})

	if err := c.DeleteSnapshot(ctx, snap.Id); err != nil {
		t.Fatalf("DeleteSnapshot() error = %v", err)
	}

	_, err := c.GetSnapshot(ctx, snap.Id)
	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "snapshot_not_found" {
		t.Errorf("GetSnapshot() after delete = %v, want snapshot_not_found", err)
	}
}

func TestClientEscapesSnapshotRefs(t *testing.T) {
	c, _, _ := newClient(t)
	_, err := c.GetSnapshot(context.Background(), "a?b#c/d")
	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an APIError rather than a corrupted request", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("status = %d, want 404 — a reserved-character ref must reach the server as one segment", apiErr.StatusCode)
	}
}
