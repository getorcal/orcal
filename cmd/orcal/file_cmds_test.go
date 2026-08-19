package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

func newCLIEnvWithListMaxEntries(t *testing.T, listMaxEntries int) *cliEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	sandboxes := sandbox.NewService(st.Sandboxes(), f,
		sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512},
		sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"})
	execs, err := exec.NewService(st.Execs(), sandboxes, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("exec.NewService() error = %v", err)
	}
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, f)
	sandboxes.SetSnapshots(snapshots)
	fileSvc := files.NewService(sandboxes, f, files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   listMaxEntries,
		ListMaxScanBytes: 1 << 20,
	})

	tokens := auth.NewService(auth.NewMemoryRepo())
	_, plaintext, err := tokens.Create(context.Background(), auth.CreateOptions{Name: "cli", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("mint cli token: %v", err)
	}
	cliToken = plaintext

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		Snapshots: snapshots,
		Files:     fileSvc,
		Tokens:    tokens,
		Audit:     audit.NewService(audit.NewMemoryRepo(), audit.RetentionPolicy{}),
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	return &cliEnv{url: srv.URL, fake: f, execs: execs}
}

func TestCLIFileListShowsSeededNames(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)
	env.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))
	env.fake.Seed(runtimeID, "/app/b.txt", 0o644, []byte("world"))

	stdout, stderr, code := env.run(t, "file", "ls", "my-agent", "/app")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "a.txt") || !strings.Contains(stdout, "b.txt") {
		t.Errorf("stdout = %q, want a.txt and b.txt", stdout)
	}
}

func TestCLIFileListPrintsTruncatedMarker(t *testing.T) {
	env := newCLIEnvWithListMaxEntries(t, 1)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)
	env.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))
	env.fake.Seed(runtimeID, "/app/b.txt", 0o644, []byte("world"))

	stdout, stderr, code := env.run(t, "file", "ls", "my-agent", "/app")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "(truncated)") {
		t.Errorf("stdout = %q, want it to contain (truncated)", stdout)
	}
}

func TestCLIFileStatHumanOutputShowsModeAndSize(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))

	stdout, stderr, code := env.run(t, "file", "stat", "my-agent", "/app/a.txt")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "0644") {
		t.Errorf("stdout = %q, want the mode 0644", stdout)
	}
	if !strings.Contains(stdout, "5") {
		t.Errorf("stdout = %q, want the size 5", stdout)
	}
}

func TestCLIFileStatMissingPathExitsNotFound(t *testing.T) {
	env := newCLIEnv(t)
	createSandboxForCP(t, env, "my-agent")

	_, stderr, code := env.run(t, "file", "stat", "my-agent", "/nope")
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if !strings.Contains(stderr, "path_not_found") {
		t.Errorf("stderr = %q, want path_not_found", stderr)
	}
}
