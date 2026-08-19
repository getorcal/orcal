package orcalclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestClientListSandboxSnapshotsScopesToSandbox(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "other-agent", Image: "alpine:3.20"})

	v1, err := c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "v1"})
	if err != nil {
		t.Fatalf("CreateSnapshot(v1) error = %v", err)
	}
	v2, err := c.CreateSnapshot(ctx, "my-agent", orcalclient.CreateSnapshotParams{Name: "v2"})
	if err != nil {
		t.Fatalf("CreateSnapshot(v2) error = %v", err)
	}
	if _, err := c.CreateSnapshot(ctx, "other-agent", orcalclient.CreateSnapshotParams{Name: "other-v1"}); err != nil {
		t.Fatalf("CreateSnapshot(other-v1) error = %v", err)
	}

	list, err := c.ListSandboxSnapshots(ctx, "my-agent", orcalclient.ListParams{})
	if err != nil {
		t.Fatalf("ListSandboxSnapshots() error = %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2, got %v", len(list.Items), list.Items)
	}
	got := map[string]bool{}
	for _, item := range list.Items {
		got[item.Id] = true
	}
	if !got[v1.Id] || !got[v2.Id] {
		t.Errorf("items = %v, want v1 (%s) and v2 (%s) and nothing from other-agent", list.Items, v1.Id, v2.Id)
	}
}

func TestClientListSandboxSnapshotsEscapesSandboxRef(t *testing.T) {
	var gotPath, gotRawQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := orcalclient.New(srv.URL, "token")

	ref := "a?b#c/d"
	if _, err := c.ListSandboxSnapshots(context.Background(), ref, orcalclient.ListParams{}); err != nil {
		t.Fatalf("ListSandboxSnapshots() error = %v", err)
	}

	wantPath := "/v1/sandboxes/" + ref + "/snapshots"
	if gotPath != wantPath {
		t.Errorf("server received path %q, want %q — a reserved-character ref must reach the server as one segment", gotPath, wantPath)
	}
	if gotRawQuery != "" {
		t.Errorf("server received query %q, want empty", gotRawQuery)
	}
}

func TestClientEscapesSnapshotRefs(t *testing.T) {
	c, _, _ := newClient(t)
	_, err := c.GetSnapshot(context.Background(), "a?b#c/d")
	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an APIError rather than a corrupted request", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 — a reserved-character ref must reach the server as one segment", apiErr.StatusCode)
	}
}
