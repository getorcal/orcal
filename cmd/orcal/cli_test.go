package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

const cliToken = "cli-token"

type cliEnv struct {
	url   string
	fake  *fake.Fake
	execs *exec.Service
}

func newCLIEnv(t *testing.T) *cliEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	sandboxes := sandbox.NewService(st.Sandboxes(), f,
		sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}, "orcal")
	execs, err := exec.NewService(st.Execs(), sandboxes, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("exec.NewService() error = %v", err)
	}
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, f)
	sandboxes.SetSnapshots(snapshots)

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		Snapshots: snapshots,
		TokenHash: auth.HashToken(cliToken),
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	return &cliEnv{url: srv.URL, fake: f, execs: execs}
}

func (e *cliEnv) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{"--url", e.url, "--token", cliToken}, args...)
	code := execute(full, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestCLICreateAndListInJSON(t *testing.T) {
	env := newCLIEnv(t)

	stdout, stderr, code := env.run(t, "create", "--name", "my-agent", "--image", "alpine", "--output", "json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create output is not JSON: %v\n%s", err, stdout)
	}
	if created["state"] != "running" {
		t.Errorf("state = %v, want running", created["state"])
	}

	listOut, _, listCode := env.run(t, "list", "--output", "json")
	if listCode != 0 {
		t.Fatalf("list exit = %d", listCode)
	}
	var list map[string]any
	json.Unmarshal([]byte(listOut), &list)
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Errorf("len(items) = %d, want 1", len(items))
	}
}

func TestCLIListHumanOutputHasAlignedHeader(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, _, code := env.run(t, "list")

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want a header plus one row:\n%s", len(lines), stdout)
	}
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "STATE") {
		t.Errorf("header = %q, want NAME and STATE columns", lines[0])
	}
	if !strings.Contains(lines[1], "my-agent") || !strings.Contains(lines[1], "running") {
		t.Errorf("row = %q, want the sandbox name and state", lines[1])
	}
}

func TestCLIExecStreamsOutputAndPropagatesExitCode(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("hello from sandbox")}},
	}, 17)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, _, code := env.run(t, "exec", "my-agent", "--", "echo", "hello")

	if !strings.Contains(stdout, "hello from sandbox") {
		t.Errorf("stdout = %q, want the streamed output", stdout)
	}
	if code != 17 {
		t.Errorf("exit = %d, want the command's own exit code 17", code)
	}
}

func TestCLIExecDetachPrintsExecID(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript(nil, 0)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, _, code := env.run(t, "exec", "my-agent", "--detach", "--", "sleep", "1")

	if code != 0 {
		t.Fatalf("exit = %d, want 0 for a detached exec", code)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("stdout is empty, want the exec id")
	}
}

func TestCLILogsReattachesToAFinishedExec(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("recorded")}},
	}, 0)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")
	detached, _, _ := env.run(t, "exec", "my-agent", "--detach", "--", "echo")
	env.execs.Wait()

	stdout, _, code := env.run(t, "logs", strings.TrimSpace(detached))

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "recorded") {
		t.Errorf("stdout = %q, want the recorded output", stdout)
	}
}

func TestCLIUnknownSandboxExitsWithNotFoundCode(t *testing.T) {
	env := newCLIEnv(t)

	_, stderr, code := env.run(t, "inspect", "ghost")

	if code != 3 {
		t.Errorf("exit = %d, want 3 for not found", code)
	}
	if !strings.Contains(stderr, "sandbox_not_found") {
		t.Errorf("stderr = %q, want the error code", stderr)
	}
}

func TestCLIBadTokenExitsWithAuthCode(t *testing.T) {
	env := newCLIEnv(t)
	var stdout, stderr bytes.Buffer

	code := execute([]string{"--url", env.url, "--token", "wrong", "list"}, &stdout, &stderr)

	if code != 4 {
		t.Errorf("exit = %d, want 4 for an auth failure", code)
	}
}

func TestCLIStopStartDestroyLifecycle(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	if _, _, code := env.run(t, "stop", "my-agent"); code != 0 {
		t.Fatalf("stop exit = %d", code)
	}
	if _, _, code := env.run(t, "start", "my-agent"); code != 0 {
		t.Fatalf("start exit = %d", code)
	}
	if _, _, code := env.run(t, "destroy", "my-agent"); code != 0 {
		t.Fatalf("destroy exit = %d", code)
	}
	if _, _, code := env.run(t, "inspect", "my-agent"); code != 3 {
		t.Errorf("inspect after destroy exit = %d, want 3", code)
	}
}
