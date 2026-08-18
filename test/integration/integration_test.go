//go:build docker

package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime/docker"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/store/sqlite"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

const (
	testImage   = "alpine:3.20"
	testToken   = "integration-token"
	testNetwork = "orcal-integration"
)

func TestMain(m *testing.M) {
	code := m.Run()
	removeTestNetwork()
	os.Exit(code)
}

func removeTestNetwork() {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return
	}
	defer cli.Close()
	cli.NetworkRemove(context.Background(), testNetwork)
}

type env struct {
	client *orcalclient.Client
	docker *docker.Docker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	rt, err := docker.New("")
	if err != nil {
		t.Skipf("no Docker daemon available: %v", err)
	}
	if err := rt.EnsureNetwork(ctx, testNetwork); err != nil {
		t.Skipf("no Docker daemon available: %v", err)
	}

	sandboxes := sandbox.NewService(st.Sandboxes(), rt,
		sandbox.Resources{CPUMillis: 1000, MemoryBytes: 512 << 20, PidsLimit: 128}, testNetwork)
	execs, err := exec.NewService(st.Execs(), sandboxes, rt, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("exec.NewService() error = %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := execs.Shutdown(shutdownCtx); err != nil {
			t.Logf("exec.Shutdown() error = %v", err)
		}
	})

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		TokenHash: auth.HashToken(testToken),
		Version:   "integration",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	return &env{client: orcalclient.New(srv.URL, testToken), docker: rt}
}

func (e *env) sandbox(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	created, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: name, Image: testImage})
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	t.Cleanup(func() { e.client.DestroySandbox(context.Background(), created.Id) })
	return created.Id
}

func (e *env) runToCompletion(t *testing.T, ref string, command ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	started, err := e.client.CreateExec(ctx, ref, orcalclient.CreateExecParams{Command: command})
	if err != nil {
		t.Fatalf("CreateExec(%v) error = %v", command, err)
	}

	var (
		output   strings.Builder
		exitCode = -1
	)
	err = e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		switch ev.Event {
		case "output":
			output.Write(ev.Data)
		case "exit":
			if ev.ExitCode != nil {
				exitCode = *ev.ExitCode
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	return output.String(), exitCode
}

func containerIDFor(t *testing.T, cli *client.Client, sandboxID string) string {
	t.Helper()
	list, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "orcal.sandbox="+sandboxID)),
	})
	if err != nil {
		t.Fatalf("ContainerList() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("found %d containers for sandbox %s, want exactly 1", len(list), sandboxID)
	}
	return list[0].ID
}

func TestCreateExecDestroyAgainstRealDocker(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-lifecycle")

	output, code := e.runToCompletion(t, "integration-lifecycle", "echo", "hello from docker")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(output, "hello from docker") {
		t.Errorf("output = %q, want the echoed text", output)
	}
}

func TestNonZeroExitCodePropagates(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-exit")

	_, code := e.runToCompletion(t, "integration-exit", "sh", "-c", "exit 42")

	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestStderrIsCapturedSeparately(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-stderr")
	ctx := context.Background()

	started, err := e.client.CreateExec(ctx, "integration-stderr", orcalclient.CreateExecParams{
		Command: []string{"sh", "-c", "echo out; echo err 1>&2"},
	})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}

	streams := map[string]string{}
	if err := e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "output" {
			streams[ev.Stream] += string(ev.Data)
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}

	if !strings.Contains(streams["stdout"], "out") {
		t.Errorf("stdout = %q, want out", streams["stdout"])
	}
	if !strings.Contains(streams["stderr"], "err") {
		t.Errorf("stderr = %q, want err", streams["stderr"])
	}
}

func TestOutputStreamResumesFromOffsetAgainstRealDocker(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-resume")
	ctx := context.Background()

	started, err := e.client.CreateExec(ctx, "integration-resume", orcalclient.CreateExecParams{
		Command: []string{"sh", "-c", "echo first; echo second"},
	})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}

	var firstOffset int64
	if err := e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "output" && firstOffset == 0 {
			firstOffset = ev.Offset
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if firstOffset == 0 {
		t.Fatal("no output frames received")
	}

	var resumed strings.Builder
	if err := e.client.StreamOutput(ctx, started.Id, firstOffset, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "output" {
			resumed.Write(ev.Data)
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if strings.Contains(resumed.String(), "first") {
		t.Errorf("resumed output = %q, want it to start after the first frame", resumed.String())
	}
}

func TestStopPreventsExecAndStartRestoresIt(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-lifecycle-gate")
	ctx := context.Background()

	if _, err := e.client.StopSandbox(ctx, "integration-lifecycle-gate"); err != nil {
		t.Fatalf("StopSandbox() error = %v", err)
	}
	if _, err := e.client.CreateExec(ctx, "integration-lifecycle-gate", orcalclient.CreateExecParams{
		Command: []string{"echo", "nope"},
	}); err == nil {
		t.Error("CreateExec() on a stopped sandbox error = nil, want a conflict")
	}

	if _, err := e.client.StartSandbox(ctx, "integration-lifecycle-gate"); err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}
	if _, code := e.runToCompletion(t, "integration-lifecycle-gate", "echo", "back"); code != 0 {
		t.Errorf("exit code after restart = %d, want 0", code)
	}
}

func TestOutputTruncationAgainstRealDocker(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-truncate")
	ctx := context.Background()

	started, err := e.client.CreateExec(ctx, "integration-truncate", orcalclient.CreateExecParams{
		Command: []string{"sh", "-c", "yes orcal | head -c 4000000"},
	})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}

	truncated := false
	if err := e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "exit" {
			truncated = ev.Truncated
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}

	final, err := e.client.GetExec(ctx, started.Id)
	if err != nil {
		t.Fatalf("GetExec() error = %v", err)
	}
	if !truncated && !final.Truncated {
		t.Errorf("truncated = %v, final.Truncated = %v, want at least one true for 4MB into a 1MiB cap", truncated, final.Truncated)
	}
	if final.State != "exited" {
		t.Errorf("state = %q, want exited", final.State)
	}
}
