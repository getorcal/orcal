package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/files"
)

func TestParseCPArg(t *testing.T) {
	cases := []struct {
		arg  string
		want cpEndpoint
	}{
		{"my-agent:/app/f.go", cpEndpoint{ref: "my-agent", path: "/app/f.go", remote: true}},
		{"./f.go", cpEndpoint{path: "./f.go"}},
		{"./a:b", cpEndpoint{path: "./a:b"}},
		{"/abs/f.go", cpEndpoint{path: "/abs/f.go"}},
		{"f.go", cpEndpoint{path: "f.go"}},
		{"a:b", cpEndpoint{ref: "a", path: "b", remote: true}},
		{`C:\tmp\f`, cpEndpoint{path: `C:\tmp\f`}},
	}
	for _, c := range cases {
		got := parseCPArg(c.arg)
		if got != c.want {
			t.Errorf("parseCPArg(%q) = %+v, want %+v", c.arg, got, c.want)
		}
	}
}

func createSandboxForCP(t *testing.T, env *cliEnv, name string) string {
	t.Helper()
	out, stderr, code := env.run(t, "create", "--name", name, "--image", "alpine:3.20", "--output", "json")
	if code != 0 {
		t.Fatalf("create exit = %d, stderr = %s", code, stderr)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create output not JSON: %v\n%s", err, out)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created id missing: %s", out)
	}
	return id
}

func TestCLICopyUploadSingleFile(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)

	dir := t.TempDir()
	localPath := filepath.Join(dir, "f.go")
	if err := os.WriteFile(localPath, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	_, stderr, code := env.run(t, "cp", localPath, "my-agent:/app/f.go")
	if code != 0 {
		t.Fatalf("cp exit = %d, stderr = %s", code, stderr)
	}

	stdout, stderr, code := env.run(t, "file", "stat", "my-agent", "/app/f.go", "--output", "json")
	if code != 0 {
		t.Fatalf("file stat exit = %d, stderr = %s", code, stderr)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("file stat output not JSON: %v\n%s", err, stdout)
	}
	if info["size"] != float64(len("package main")) {
		t.Errorf("size = %v, want %d", info["size"], len("package main"))
	}
}

func TestCLICopyDownloadSingleFile(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.Seed(runtimeID, "/app/f.go", 0o644, []byte("package main"))

	dir := t.TempDir()
	localPath := filepath.Join(dir, "out.go")

	_, stderr, code := env.run(t, "cp", "my-agent:/app/f.go", localPath)
	if code != 0 {
		t.Fatalf("cp exit = %d, stderr = %s", code, stderr)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != "package main" {
		t.Errorf("content = %q, want %q", got, "package main")
	}
}

func TestCLICopyUploadTreeRecursive(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	_, stderr, code := env.run(t, "cp", "-r", srcDir, "my-agent:/app")
	if code != 0 {
		t.Fatalf("cp -r exit = %d, stderr = %s", code, stderr)
	}

	stdout, stderr, code := env.run(t, "file", "ls", "my-agent", "/app/proj")
	if code != 0 {
		t.Fatalf("file ls exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "a.txt") {
		t.Errorf("stdout = %q, want a.txt", stdout)
	}

	stdout, stderr, code = env.run(t, "file", "ls", "my-agent", "/app/proj/sub")
	if code != 0 {
		t.Fatalf("file ls sub exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "b.txt") {
		t.Errorf("stdout = %q, want b.txt", stdout)
	}
}

func TestCLICopyUploadTreeToNonexistentDestination(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	_, stderr, code := env.run(t, "cp", "-r", srcDir, "my-agent:/app/proj")
	if code != 0 {
		t.Fatalf("cp -r exit = %d, stderr = %s", code, stderr)
	}

	stdout, stderr, code := env.run(t, "file", "ls", "my-agent", "/app/proj")
	if code != 0 {
		t.Fatalf("file ls exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "a.txt") {
		t.Errorf("stdout = %q, want a.txt", stdout)
	}

	stdout, stderr, code = env.run(t, "file", "ls", "my-agent", "/app/proj/sub")
	if code != 0 {
		t.Fatalf("file ls sub exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "b.txt") {
		t.Errorf("stdout = %q, want b.txt", stdout)
	}
}

func TestCLICopyUploadTreeRenamesAtNonexistentDestination(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	_, stderr, code := env.run(t, "cp", "-r", srcDir, "my-agent:/app/renamed")
	if code != 0 {
		t.Fatalf("cp -r exit = %d, stderr = %s", code, stderr)
	}

	stdout, stderr, code := env.run(t, "file", "stat", "my-agent", "/app/renamed/a.txt", "--output", "json")
	if code != 0 {
		t.Fatalf("file stat exit = %d, stderr = %s", code, stderr)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("file stat output not JSON: %v\n%s", err, stdout)
	}
	if info["size"] != float64(len("a")) {
		t.Errorf("size = %v, want %d", info["size"], len("a"))
	}

	if _, _, code := env.run(t, "file", "stat", "my-agent", "/app/src"); code == 0 {
		t.Error("/app/src exists; want the upload rooted at the destination's basename, not the source's")
	}
}

func TestCLICopyDownloadTreeRecursive(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)
	env.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("a"))
	env.fake.SeedDir(runtimeID, "/app/sub", 0o755)
	env.fake.Seed(runtimeID, "/app/sub/b.txt", 0o644, []byte("b"))

	dir := t.TempDir()
	dst := filepath.Join(dir, "out")

	_, stderr, code := env.run(t, "cp", "-r", "my-agent:/app", dst)
	if code != 0 {
		t.Fatalf("cp -r exit = %d, stderr = %s", code, stderr)
	}

	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(got) != "a" {
		t.Errorf("a.txt content = %q, want %q", got, "a")
	}
	got, err = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("read sub/b.txt: %v", err)
	}
	if string(got) != "b" {
		t.Errorf("sub/b.txt content = %q, want %q", got, "b")
	}
}

func TestCLICopyDownloadTreeIntoExistingLocalDirectory(t *testing.T) {
	env := newCLIEnv(t)
	sandboxID := createSandboxForCP(t, env, "my-agent")
	runtimeID := env.fake.IDForSandbox(sandboxID)
	env.fake.SeedDir(runtimeID, "/app", 0o755)
	env.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("a"))

	dst := t.TempDir()

	_, stderr, code := env.run(t, "cp", "-r", "my-agent:/app", dst)
	if code != 0 {
		t.Fatalf("cp -r exit = %d, stderr = %s", code, stderr)
	}

	got, err := os.ReadFile(filepath.Join(dst, "app", "a.txt"))
	if err != nil {
		t.Fatalf("read app/a.txt inside the existing destination dir: %v", err)
	}
	if string(got) != "a" {
		t.Errorf("app/a.txt content = %q, want %q", got, "a")
	}
}

func TestExtractArchiveRejectsEntriesEscapingTheLocalDestination(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "../evil.txt",
		Mode:     0o644,
		Size:     int64(len("payload")),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("payload")); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	dst := t.TempDir()
	err := extractArchive(&buf, dst, true)
	if !errors.Is(err, files.ErrUnsafeEntry) {
		t.Fatalf("extractArchive() error = %v, want files.ErrUnsafeEntry", err)
	}

	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dst), "evil.txt")); !os.IsNotExist(statErr) {
		t.Error("evil.txt was written outside the destination directory")
	}
}

func TestCopyRecursiveEmptyRemoteDirCreatesLocalDir(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "app/",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := extractArchive(&buf, dst, true); err != nil {
		t.Fatalf("extractArchive() error = %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v, want the empty directory to have been created", dst, err)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", dst)
	}
}

func TestCopyRecursiveSingleRemoteFileStaysAFile(t *testing.T) {
	body := []byte("package main")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "f.go",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out.go")
	if err := extractArchive(&buf, dst, true); err != nil {
		t.Fatalf("extractArchive() error = %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v, want the file to have been created", dst, err)
	}
	if info.IsDir() {
		t.Fatalf("%q is a directory, want a regular file", dst)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read %q: %v", dst, err)
	}
	if string(got) != string(body) {
		t.Errorf("content = %q, want %q", got, body)
	}
}

func TestCLICopyZeroRemoteEndpointsExitsUsageCode(t *testing.T) {
	env := newCLIEnv(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	dst := filepath.Join(dir, "b.txt")

	_, stderr, code := env.run(t, "cp", src, dst)
	if code != 2 {
		t.Errorf("exit = %d, want 2 for zero remote endpoints", code)
	}
	if !strings.Contains(stderr, "got zero") {
		t.Errorf("stderr = %q, want it to mention zero remote endpoints", stderr)
	}
}

func TestCLICopyTwoRemoteEndpointsExitsUsageCode(t *testing.T) {
	env := newCLIEnv(t)
	createSandboxForCP(t, env, "my-agent")
	createSandboxForCP(t, env, "other-agent")

	_, stderr, code := env.run(t, "cp", "my-agent:/app/a.txt", "other-agent:/app/b.txt")
	if code != 2 {
		t.Errorf("exit = %d, want 2 for two remote endpoints", code)
	}
	if !strings.Contains(stderr, "got two") {
		t.Errorf("stderr = %q, want it to mention two remote endpoints", stderr)
	}
}

func TestCLICopyLocalDirectorySourceWithoutRecursiveExitsUsageCode(t *testing.T) {
	env := newCLIEnv(t)
	createSandboxForCP(t, env, "my-agent")
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, stderr, code := env.run(t, "cp", srcDir, "my-agent:/app")
	if code != 2 {
		t.Errorf("exit = %d, want 2 for a local directory source without -r", code)
	}
	if !strings.Contains(stderr, "-r") {
		t.Errorf("stderr = %q, want it to mention -r", stderr)
	}
}
