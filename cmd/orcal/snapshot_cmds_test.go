package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLISnapshotCreateAndList(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine:3.20")

	out, stderr, code := env.run(t, "snapshot", "create", "my-agent", "--name", "working-v1", "--output", "json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if snap["name"] != "working-v1" {
		t.Errorf("name = %v, want working-v1", snap["name"])
	}

	listOut, _, listCode := env.run(t, "snapshot", "list", "--sandbox", "my-agent")
	if listCode != 0 {
		t.Fatalf("list exit = %d", listCode)
	}
	if !strings.Contains(listOut, "working-v1") {
		t.Errorf("list output = %q, want it to contain working-v1", listOut)
	}
	if !strings.Contains(listOut, "SIZE") {
		t.Errorf("list header = %q, want a SIZE column", listOut)
	}
}

func TestCLIForkCreatesASandboxFromASnapshot(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine:3.20")
	env.run(t, "snapshot", "create", "my-agent", "--name", "v1")

	out, stderr, code := env.run(t, "fork", "v1", "--name", "experiment-a", "--output", "json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	var sb map[string]any
	json.Unmarshal([]byte(out), &sb)
	if sb["state"] != "running" {
		t.Errorf("state = %v, want running", sb["state"])
	}
	if sb["parent_snapshot_id"] == nil {
		t.Error("parent_snapshot_id is null, want the snapshot id")
	}
}

func TestCLIRestoreRequiresConfirmationUnlessYes(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine:3.20")
	env.run(t, "snapshot", "create", "my-agent", "--name", "v1")

	_, stderr, code := env.run(t, "restore", "my-agent", "v1")
	if code == 0 {
		t.Error("restore without --yes exited 0, want a refusal")
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr = %q, want it to mention --yes", stderr)
	}

	_, stderr, code = env.run(t, "restore", "my-agent", "v1", "--yes")
	if code != 0 {
		t.Fatalf("restore --yes exit = %d, stderr = %s", code, stderr)
	}
}

func TestCLISnapshotDelete(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine:3.20")
	env.run(t, "snapshot", "create", "my-agent", "--name", "v1")

	if _, _, code := env.run(t, "snapshot", "delete", "v1"); code != 0 {
		t.Fatalf("delete exit = %d", code)
	}
	if _, _, code := env.run(t, "snapshot", "inspect", "v1"); code != 3 {
		t.Errorf("inspect after delete exit = %d, want 3", code)
	}
}
