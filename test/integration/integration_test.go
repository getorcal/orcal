//go:build docker

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

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
		Command: []string{"sh", "-c", "echo first; sleep 1; echo second"},
	})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}

	var (
		firstOffset int64
		frameCount  int
	)
	if err := e.client.StreamOutput(ctx, started.Id, 0, func(ev orcalclient.OutputEvent) error {
		if ev.Event == "output" {
			frameCount++
			if firstOffset == 0 {
				firstOffset = ev.Offset
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if frameCount < 2 {
		t.Fatalf("received %d output frames, want at least 2 so the resume spans a real reconnect", frameCount)
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
	if !strings.Contains(resumed.String(), "second") {
		t.Errorf("resumed output = %q, want it to contain the second frame", resumed.String())
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
