package main

import (
	"encoding/json"
	"errors"
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
