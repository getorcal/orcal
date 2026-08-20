//go:build docker

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestSnapshotLeavesTheSandboxRunning(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "snap-running")
	e.snapshot(t, "snap-running", "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started, err := e.client.CreateExec(ctx, "snap-running", orcalclient.CreateExecParams{Command: []string{"echo", "alive"}})
	if err != nil {
		t.Fatalf("CreateExec after snapshot error = %v", err)
	}
	var out strings.Builder
	code := -1
	err = e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "output" {
			out.Write(ev.Data)
		}
		if ev.Event == "exit" && ev.ExitCode != nil {
			code = *ev.ExitCode
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput error = %v (a container left paused would hang until this context expires)", err)
	}
	if code != 0 || !strings.Contains(out.String(), "alive") {
		t.Errorf("exit=%d out=%q, want 0 and alive — the sandbox may be paused", code, out.String())
	}
}

func TestSnapshotFailureLeavesTheSandboxUnpaused(t *testing.T) {
	e := newEnv(t)
	id := e.sandbox(t, "snap-failure")
	ctx := context.Background()

	if _, err := e.client.DestroySandbox(ctx, id); err != nil {
		t.Fatalf("DestroySandbox() error = %v", err)
	}
	if _, err := e.client.CreateSnapshot(ctx, id, orcalclient.CreateSnapshotParams{}); err == nil {
		t.Fatal("CreateSnapshot on a destroyed sandbox error = nil, want a failure")
	}

	fresh := e.sandbox(t, "snap-failure-2")
	if _, code := e.runToCompletion(t, fresh, "echo", "ok"); code != 0 {
		t.Errorf("exit = %d, want 0 — a failed snapshot must not leave the daemon or host wedged", code)
	}
}

func TestForkStartsFromTheSnapshotFilesystem(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "fork-parent")
	e.runToCompletion(t, "fork-parent", "sh", "-c", "echo original > /tmp/marker")
	e.snapshot(t, "fork-parent", "fork-base")
	e.runToCompletion(t, "fork-parent", "sh", "-c", "echo changed > /tmp/marker")

	ctx := context.Background()
	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "fork-child", Snapshot: "fork-base"})
	if err != nil {
		t.Fatalf("fork error = %v", err)
	}
	t.Cleanup(func() { e.client.DestroySandbox(context.Background(), forked.Id) })

	out, code := e.runToCompletion(t, forked.Id, "cat", "/tmp/marker")
	if code != 0 {
		t.Fatalf("cat exit = %d", code)
	}
	if strings.TrimSpace(out) != "original" {
		t.Errorf("marker = %q, want original — the fork came from live state, not the snapshot", strings.TrimSpace(out))
	}
	if forked.ParentSnapshotId == nil {
		t.Error("parent_snapshot_id is nil, want the snapshot id")
	}
}

func TestForkIsIsolatedFromItsParent(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "iso-parent")
	e.snapshot(t, "iso-parent", "iso-base")

	ctx := context.Background()
	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "iso-child", Snapshot: "iso-base"})
	if err != nil {
		t.Fatalf("fork error = %v", err)
	}
	t.Cleanup(func() { e.client.DestroySandbox(context.Background(), forked.Id) })

	e.runToCompletion(t, forked.Id, "sh", "-c", "echo child > /tmp/fork-only")

	if _, code := e.runToCompletion(t, "iso-parent", "test", "-f", "/tmp/fork-only"); code == 0 {
		t.Error("parent sees the fork's file; the two are not isolated")
	}
}

func TestRestoreDiscardsPostSnapshotChanges(t *testing.T) {
	e := newEnv(t)
	id := e.sandbox(t, "restore-target")
	e.snapshot(t, "restore-target", "restore-base")
	e.runToCompletion(t, "restore-target", "sh", "-c", "echo later > /tmp/after")

	if _, code := e.runToCompletion(t, "restore-target", "test", "-f", "/tmp/after"); code != 0 {
		t.Fatal("setup failed: /tmp/after should exist before the restore")
	}

	restored, err := e.client.RestoreSandbox(context.Background(), "restore-target", "restore-base")
	if err != nil {
		t.Fatalf("RestoreSandbox() error = %v", err)
	}
	if restored.Id != id {
		t.Errorf("id = %s, want %s — restore must keep the sandbox's identity", restored.Id, id)
	}

	if _, code := e.runToCompletion(t, "restore-target", "test", "-f", "/tmp/after"); code == 0 {
		t.Error("/tmp/after survived the restore")
	}
	if _, code := e.runToCompletion(t, "restore-target", "echo", "alive"); code != 0 {
		t.Error("sandbox does not run execs after a restore")
	}
}

func TestRestoreFromAnotherSandboxsSnapshotIsPermitted(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "donor")
	e.runToCompletion(t, "donor", "sh", "-c", "echo donated > /tmp/from-donor")
	e.snapshot(t, "donor", "donor-base")

	e.sandbox(t, "recipient")
	if _, err := e.client.RestoreSandbox(context.Background(), "recipient", "donor-base"); err != nil {
		t.Fatalf("restore across sandboxes error = %v, want it permitted", err)
	}

	out, code := e.runToCompletion(t, "recipient", "cat", "/tmp/from-donor")
	if code != 0 || strings.TrimSpace(out) != "donated" {
		t.Errorf("out=%q exit=%d, want donated/0", strings.TrimSpace(out), code)
	}
}

func TestDeleteSnapshotIsRefusedWhileADescendantExists(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.sandbox(t, "lineage-root")
	rootID := e.snapshot(t, "lineage-root", "lineage-a")

	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "lineage-fork", Snapshot: "lineage-a"})
	if err != nil {
		t.Fatalf("fork error = %v", err)
	}
	t.Cleanup(func() { e.client.DestroySandbox(context.Background(), forked.Id) })
	childID := e.snapshot(t, forked.Id, "lineage-b")

	err = e.client.DeleteSnapshot(ctx, rootID)
	var apiErr *orcalclient.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("DeleteSnapshot(parent) = %v, want a 409", err)
	}
	if _, err := e.client.GetSnapshot(ctx, rootID); err != nil {
		t.Errorf("parent unreadable after a refused delete: %v", err)
	}

	if err := e.client.DeleteSnapshot(ctx, childID); err != nil {
		t.Fatalf("DeleteSnapshot(child) error = %v", err)
	}
	if _, err := e.client.DestroySandbox(ctx, forked.Id); err != nil {
		t.Fatalf("DestroySandbox(fork) error = %v", err)
	}
	if err := e.client.DeleteSnapshot(ctx, rootID); err != nil {
		t.Errorf("DeleteSnapshot(parent) after the descendant is gone = %v, want nil", err)
	}
}

func TestSnapshotSurvivesItsSandbox(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.sandbox(t, "ephemeral")
	e.runToCompletion(t, "ephemeral", "sh", "-c", "echo kept > /tmp/kept")
	e.snapshot(t, "ephemeral", "survives")

	if _, err := e.client.DestroySandbox(ctx, id); err != nil {
		t.Fatalf("DestroySandbox() error = %v", err)
	}
	if _, err := e.client.GetSnapshot(ctx, "survives"); err != nil {
		t.Fatalf("snapshot unreadable after its sandbox was destroyed: %v", err)
	}

	revived, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "revived", Snapshot: "survives"})
	if err != nil {
		t.Fatalf("fork from an orphaned snapshot error = %v", err)
	}
	t.Cleanup(func() { e.client.DestroySandbox(context.Background(), revived.Id) })

	out, code := e.runToCompletion(t, revived.Id, "cat", "/tmp/kept")
	if code != 0 || strings.TrimSpace(out) != "kept" {
		t.Errorf("out=%q exit=%d, want kept/0", strings.TrimSpace(out), code)
	}
}

func TestSnapshotOfAStoppedSandboxSucceeds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.sandbox(t, "stopped-snap")
	if _, err := e.client.StopSandbox(ctx, "stopped-snap"); err != nil {
		t.Fatalf("StopSandbox() error = %v", err)
	}

	e.snapshot(t, "stopped-snap", "from-stopped")

	got, err := e.client.GetSandbox(ctx, "stopped-snap")
	if err != nil {
		t.Fatalf("GetSandbox() error = %v", err)
	}
	if got.State != "stopped" {
		t.Errorf("state = %q, want stopped — snapshotting must not start a stopped sandbox", got.State)
	}
}
