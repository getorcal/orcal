package files_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/files"
)

func TestWriteCreatesFileInExistingDirectory(t *testing.T) {
	svc, f, _ := newService(t)
	ctx := context.Background()

	if err := svc.Write(ctx, "my-agent", "/app/new.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	rc, info, err := svc.Read(ctx, "my-agent", "/app/new.txt")
	if err != nil {
		t.Fatalf("Read() after Write error = %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", body)
	}
	if info.Size != 5 {
		t.Errorf("Size = %d, want 5", info.Size)
	}
	_ = f
}

func TestWriteCreatesMissingParents(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if err := svc.Write(ctx, "my-agent", "/app/deep/nested/f.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	for _, dir := range []string{"/app/deep", "/app/deep/nested"} {
		info, err := svc.Stat(ctx, "my-agent", dir)
		if err != nil {
			t.Errorf("Stat(%q) error = %v, want the directory to have been created", dir, err)
			continue
		}
		if !info.IsDir {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestWriteDoesNotDisturbAnExistingParent(t *testing.T) {
	svc, f, access := newService(t)
	ctx := context.Background()

	f.SeedDir(access.RuntimeID(), "/app", 0o701)
	before, err := svc.Stat(ctx, "my-agent", "/app")
	if err != nil {
		t.Fatalf("Stat(/app) error = %v", err)
	}

	if err := svc.Write(ctx, "my-agent", "/app/sub/f.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	after, err := svc.Stat(ctx, "my-agent", "/app")
	if err != nil {
		t.Fatalf("Stat(/app) after write error = %v", err)
	}
	if after.Mode.Perm() != before.Mode.Perm() {
		t.Errorf("/app mode changed from %v to %v; the tar must not contain an entry for an existing parent",
			before.Mode.Perm(), after.Mode.Perm())
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if err := svc.Write(ctx, "my-agent", "/app/main.go", strings.NewReader("replaced")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	rc, _, _ := svc.Read(ctx, "my-agent", "/app/main.go")
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "replaced" {
		t.Errorf("body = %q, want replaced", body)
	}
}

func TestWriteTakesTheLock(t *testing.T) {
	svc, _, access := newService(t)

	if err := svc.Write(context.Background(), "my-agent", "/app/x.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if access.locked != 1 {
		t.Errorf("locked = %d, want 1; a write racing a snapshot would commit a partial tree", access.locked)
	}
}

func TestWriteRejectsBodyOverLimit(t *testing.T) {
	svc, _, _ := newServiceWithLimits(t, files.Limits{
		FileMaxBytes:     4,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   100,
		ListMaxScanBytes: 1 << 20,
	})

	err := svc.Write(context.Background(), "my-agent", "/app/big.txt", strings.NewReader("12345"))
	if !errors.Is(err, files.ErrTooLarge) {
		t.Errorf("Write() error = %v, want ErrTooLarge", err)
	}
	if _, statErr := svc.Stat(context.Background(), "my-agent", "/app/big.txt"); statErr == nil {
		t.Error("an over-limit write left a file behind; nothing must be written when the limit trips")
	}
}

func TestWriteRefusesToClobberADirectory(t *testing.T) {
	svc, f, access := newService(t)
	ctx := context.Background()
	f.SeedDir(access.RuntimeID(), "/app/worktree", 0o755)
	f.Seed(access.RuntimeID(), "/app/worktree/important.go", 0o644, []byte("valuable"))

	err := svc.Write(ctx, "my-agent", "/app/worktree", strings.NewReader("clobber"))
	if !errors.Is(err, files.ErrNotRegular) {
		t.Fatalf("Write() onto a directory = %v, want ErrNotRegular", err)
	}

	info, err := svc.Stat(ctx, "my-agent", "/app/worktree")
	if err != nil {
		t.Fatalf("Stat() after refused write = %v", err)
	}
	if !info.IsDir {
		t.Error("/app/worktree is no longer a directory; a refused write must change nothing")
	}
	if _, err := svc.Stat(ctx, "my-agent", "/app/worktree/important.go"); err != nil {
		t.Errorf("child was destroyed: %v", err)
	}
}

func TestWriteRejectsRelativePath(t *testing.T) {
	svc, _, _ := newService(t)

	if err := svc.Write(context.Background(), "my-agent", "app/x.txt", strings.NewReader("x")); !errors.Is(err, files.ErrInvalidPath) {
		t.Errorf("Write() error = %v, want ErrInvalidPath", err)
	}
}
