//go:build gvisor

package integration

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

var probeCommand = []string{"sh", "-c",
	"if command -v dmesg >/dev/null 2>&1 && dmesg 2>/dev/null | grep -qi gvisor; " +
		"then dmesg 2>/dev/null | grep -i gvisor; else cat /proc/version; fi"}

func (e *env) createSandbox(t *testing.T, name string) *apigen.Sandbox {
	t.Helper()
	created, err := e.client.CreateSandbox(context.Background(), orcalclient.CreateSandboxParams{Name: name, Image: testImage})
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	e.sandboxes = append(e.sandboxes, created.Id)
	return created
}

func (e *env) execOutput(t *testing.T, sandboxID string, command []string) string {
	t.Helper()
	output, code := e.runToCompletion(t, sandboxID, command...)
	if code != 0 {
		t.Fatalf("probe command %v exited %d, want 0; output = %q", command, code, output)
	}
	return strings.TrimSpace(output)
}

func runProbeUnderDefaultRuntime(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	if _, err := cli.ImageInspect(ctx, testImage); err != nil {
		reader, err := cli.ImagePull(ctx, testImage, image.PullOptions{})
		if err != nil {
			t.Fatalf("ImagePull(%s) error = %v", testImage, err)
		}
		_, copyErr := io.Copy(io.Discard, reader)
		reader.Close()
		if copyErr != nil {
			t.Fatalf("drain image pull: %v", copyErr)
		}
	}

	created, err := cli.ContainerCreate(ctx,
		&container.Config{Image: testImage, Cmd: []string{"sleep", "infinity"}, Labels: map[string]string{"orcal.managed": "true"}},
		&container.HostConfig{Runtime: ""}, nil, nil, "")
	if err != nil {
		t.Fatalf("ContainerCreate() error = %v", err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})

	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("ContainerStart() error = %v", err)
	}

	execCreated, err := cli.ContainerExecCreate(ctx, created.ID, container.ExecOptions{
		Cmd: probeCommand, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		t.Fatalf("ContainerExecCreate() error = %v", err)
	}
	attached, err := cli.ContainerExecAttach(ctx, execCreated.ID, container.ExecAttachOptions{})
	if err != nil {
		t.Fatalf("ContainerExecAttach() error = %v", err)
	}
	defer attached.Close()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, attached.Reader); err != nil {
		t.Fatalf("read exec output: %v", err)
	}
	return strings.TrimSpace(buf.String())
}

func TestSandboxActuallyRunsUnderGvisor(t *testing.T) {
	e := newEnv(t)

	sb := e.createSandbox(t, "gvisor-probe")
	if sb.OciRuntime == nil || *sb.OciRuntime != "runsc" {
		t.Fatalf("expected oci_runtime runsc, got %v", sb.OciRuntime)
	}

	underGvisor := e.execOutput(t, sb.Id, probeCommand)
	underDefault := runProbeUnderDefaultRuntime(t)

	if underGvisor == underDefault {
		t.Fatalf("the probe returns %q under both runtimes, so it proves nothing; pick a probe that distinguishes them", underGvisor)
	}
	if !strings.Contains(strings.ToLower(underGvisor), "gvisor") {
		t.Fatalf("probe output does not identify gVisor: %q", underGvisor)
	}
}

func TestProductWorksEndToEndUnderGvisor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	ref := e.sandbox(t, "gvisor-lifecycle")
	if _, code := e.runToCompletion(t, ref, "sh", "-c", "echo original > /tmp/marker"); code != 0 {
		t.Fatalf("seed marker exit = %d, want 0", code)
	}

	content := []byte("hello under gvisor\n")
	if err := e.client.WriteFile(ctx, ref, "/tmp/hello.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rc, err := e.client.ReadFile(ctx, ref, "/tmp/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	archiveRC, err := e.client.DownloadArchive(ctx, ref, "/tmp")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	if !archiveContains(t, archiveRC, "tmp/hello.txt") {
		t.Error("downloaded archive is missing hello.txt")
	}

	snapID := e.snapshot(t, ref, "gvisor-base")

	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "gvisor-fork", Snapshot: snapID})
	if err != nil {
		t.Fatalf("fork error = %v", err)
	}
	e.sandboxes = append(e.sandboxes, forked.Id)
	if forked.OciRuntime == nil || *forked.OciRuntime != "runsc" {
		t.Errorf("forked oci_runtime = %v, want runsc", forked.OciRuntime)
	}
	if out, code := e.runToCompletion(t, forked.Id, "cat", "/tmp/marker"); code != 0 || strings.TrimSpace(out) != "original" {
		t.Errorf("fork marker = %q code=%d, want original/0", out, code)
	}

	restored, err := e.client.RestoreSandbox(ctx, ref, "gvisor-base")
	if err != nil {
		t.Fatalf("RestoreSandbox() error = %v", err)
	}
	if restored.OciRuntime == nil || *restored.OciRuntime != "runsc" {
		t.Errorf("restored oci_runtime = %v, want runsc", restored.OciRuntime)
	}
	if out, code := e.runToCompletion(t, ref, "cat", "/tmp/marker"); code != 0 || strings.TrimSpace(out) != "original" {
		t.Errorf("restored marker = %q code=%d, want original/0", out, code)
	}
}
