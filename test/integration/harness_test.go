//go:build docker || gvisor

package integration

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime/docker"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

const (
	testImage   = "alpine:3.20"
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
	cli.NetworkRemove(context.Background(), testNetwork+"-isolated")
}

type env struct {
	client    *orcalclient.Client
	docker    *docker.Docker
	sandboxes []string
	snapshots []string
	images    []string
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
	if err := rt.EnsureNetwork(ctx, testNetwork, false); err != nil {
		t.Skipf("no Docker daemon available: %v", err)
	}
	if err := rt.EnsureNetwork(ctx, testNetwork+"-isolated", true); err != nil {
		t.Skipf("no Docker daemon available: %v", err)
	}

	ociRuntime, err := rt.ResolveRuntime(ctx, os.Getenv("ORCAL_CONTAINER_RUNTIME"))
	if err != nil {
		t.Fatalf("ResolveRuntime() error = %v", err)
	}

	sandboxes := sandbox.NewService(st.Sandboxes(), rt,
		sandbox.Resources{CPUMillis: 1000, MemoryBytes: 512 << 20, PidsLimit: 128},
		sandbox.Networks{Full: testNetwork, Isolated: testNetwork + "-isolated"}, ociRuntime)
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
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, rt)
	sandboxes.SetSnapshots(snapshots)
	fileSvc := files.NewService(sandboxes, rt, files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   1000,
		ListMaxScanBytes: 1 << 20,
	})

	tokens := auth.NewService(auth.NewMemoryRepo())
	_, testToken, err := tokens.Create(ctx, auth.CreateOptions{Name: "integration", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("mint integration token: %v", err)
	}

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		Snapshots: snapshots,
		Files:     fileSvc,
		Tokens:    tokens,
		Audit:     audit.NewService(audit.NewMemoryRepo(), audit.RetentionPolicy{}),
		Version:   "integration",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	e := &env{client: orcalclient.New(srv.URL, testToken), docker: rt}
	t.Cleanup(func() {
		teardownCtx := context.Background()
		for _, id := range e.sandboxes {
			if _, err := e.client.DestroySandbox(teardownCtx, id); err != nil && !sandboxAlreadyGone(err) {
				t.Errorf("cleanup: DestroySandbox(%s) error = %v", id, err)
			}
		}
		for _, v := range slices.Backward(e.snapshots) {
			id := v
			if err := e.client.DeleteSnapshot(teardownCtx, id); err != nil && !snapshotAlreadyGone(err) {
				t.Errorf("cleanup: DeleteSnapshot(%s) error = %v", id, err)
			}
		}
		if len(e.images) == 0 {
			return
		}
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			t.Errorf("cleanup: docker client: %v", err)
			return
		}
		defer cli.Close()
		for _, ref := range e.images {
			if _, err := cli.ImageRemove(teardownCtx, ref, image.RemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
				t.Errorf("cleanup: ImageRemove(%s) error = %v", ref, err)
			}
		}
	})
	return e
}

func sandboxAlreadyGone(err error) bool {
	var apiErr *orcalclient.APIError
	if !asAPIError(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusConflict
}

func snapshotAlreadyGone(err error) bool {
	var apiErr *orcalclient.APIError
	if !asAPIError(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

func (e *env) sandbox(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	created, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: name, Image: testImage})
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	e.sandboxes = append(e.sandboxes, created.Id)
	return created.Id
}

func (e *env) sandboxWithImage(t *testing.T, name, img string) string {
	t.Helper()
	ctx := context.Background()
	created, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: name, Image: img})
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	e.sandboxes = append(e.sandboxes, created.Id)
	e.images = append(e.images, img)
	return created.Id
}

func (e *env) snapshot(t *testing.T, sandboxRef, name string) string {
	t.Helper()
	snap, err := e.client.CreateSnapshot(context.Background(), sandboxRef, orcalclient.CreateSnapshotParams{Name: name})
	if err != nil {
		t.Fatalf("CreateSnapshot(%s) error = %v", sandboxRef, err)
	}
	e.snapshots = append(e.snapshots, snap.Id)
	return snap.Id
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

func asAPIError(err error, target **orcalclient.APIError) bool {
	return errors.As(err, target)
}

func archiveContains(t *testing.T, rc io.ReadCloser, name string) bool {
	t.Helper()
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		if h.Name == name {
			return true
		}
	}
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
