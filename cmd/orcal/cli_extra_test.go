package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
)

func TestCLIExecSurfacesGapOnStderrWithoutFailingTheCommand(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("before the gap")}},
		{Err: errors.New("simulated runtime disconnect")},
	}, 0)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, stderr, code := env.run(t, "exec", "my-agent", "--", "echo", "hi")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "before the gap") {
		t.Errorf("stdout = %q, want the output recorded before the gap", stdout)
	}
	if !strings.Contains(stderr, "gap") {
		t.Errorf("stderr = %q, want a gap warning", stderr)
	}
}

func TestCLIExecGapDiagnosticIsJSONWhenOutputIsJSON(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript([]fake.Step{
		{Err: errors.New("simulated runtime disconnect")},
	}, 3)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	_, stderr, code := env.run(t, "exec", "my-agent", "--output", "json", "--", "echo", "hi")

	if code != 3 {
		t.Fatalf("exit = %d, want the command's own exit code 3", code)
	}
	dec := json.NewDecoder(strings.NewReader(stderr))
	var sawGap bool
	for {
		var payload map[string]any
		if err := dec.Decode(&payload); err != nil {
			break
		}
		if payload["event"] == "gap" {
			sawGap = true
		}
	}
	if !sawGap {
		t.Errorf("stderr = %q, want a JSON gap event", stderr)
	}
}

func TestCLIMissingRequiredImageExitsWithUsageCode(t *testing.T) {
	env := newCLIEnv(t)

	_, _, code := env.run(t, "create", "--name", "my-agent")

	if code != 2 {
		t.Errorf("exit = %d, want 2 for a missing required flag", code)
	}
}

func TestCLIInvalidOutputValueExitsWithUsageCode(t *testing.T) {
	env := newCLIEnv(t)

	_, _, code := env.run(t, "list", "--output", "yaml")

	if code != 2 {
		t.Errorf("exit = %d, want 2 for an invalid --output value", code)
	}
}

func TestCLIWrongPositionalArgCountExitsWithUsageCode(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	_, _, code := env.run(t, "exec", "my-agent")

	if code != 2 {
		t.Errorf("exit = %d, want 2 for a missing exec command", code)
	}
}

func TestCLIExplicitConfigThatCannotBeReadExitsWithUsageCode(t *testing.T) {
	env := newCLIEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	var stdout, stderr bytes.Buffer

	code := execute([]string{"--url", env.url, "--token", cliToken, "--config", missing, "list"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit = %d, want 2 for an unreadable explicit config path", code)
	}
	if !strings.Contains(stderr.String(), missing) {
		t.Errorf("stderr = %q, want it to name the path %q", stderr.String(), missing)
	}
}

func TestCLIDefaultConfigPathThatDoesNotExistStillSucceeds(t *testing.T) {
	env := newCLIEnv(t)

	_, _, code := env.run(t, "list")

	if code != 0 {
		t.Errorf("exit = %d, want 0 - a missing default config path must not fail the command", code)
	}
}

func TestCLIInspectHumanOutputShowsNameAndState(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, _, code := env.run(t, "inspect", "my-agent")

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var probe any
	if err := json.Unmarshal([]byte(stdout), &probe); err == nil {
		t.Fatalf("inspect --output human produced JSON:\n%s", stdout)
	}
	if !strings.Contains(stdout, "my-agent") || !strings.Contains(stdout, "running") {
		t.Errorf("stdout = %q, want the sandbox name and state", stdout)
	}
}

func TestCLIInspectJSONOutputStillParsesAsJSON(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	stdout, _, code := env.run(t, "inspect", "my-agent", "--output", "json")

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("inspect --output json is not JSON: %v\n%s", err, stdout)
	}
	if got["state"] != "running" {
		t.Errorf("state = %v, want running", got["state"])
	}
}

func TestCLILogsDoesNotAdoptTheExecsOwnNonZeroExitCode(t *testing.T) {
	env := newCLIEnv(t)
	env.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("boom")}},
	}, 17)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine")

	detached, _, _ := env.run(t, "exec", "my-agent", "--detach", "--", "false")
	env.execs.Wait()

	logsStdout, _, logsCode := env.run(t, "logs", strings.TrimSpace(detached))
	if logsCode != 0 {
		t.Fatalf("logs exit = %d, want 0 even though the exec exited 17", logsCode)
	}
	if !strings.Contains(logsStdout, "boom") {
		t.Errorf("logs stdout = %q, want the recorded output", logsStdout)
	}

	env.run(t, "create", "--name", "second-agent", "--image", "alpine")
	_, _, execCode := env.run(t, "exec", "second-agent", "--", "false")
	if execCode != 17 {
		t.Fatalf("exec exit = %d, want the command's own exit code 17", execCode)
	}
}
