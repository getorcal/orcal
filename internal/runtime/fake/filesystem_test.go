package fake

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/getorcal/orcal/internal/runtime"
)

func runningWithFile(t *testing.T) (*Fake, string) {
	t.Helper()
	f := New()
	ctx := context.Background()
	id, err := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := f.Start(ctx, id); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	f.Seed(id, "/app/main.go", 0o644, []byte("package main"))
	return f, id
}

func TestStatPathReportsSizeAndMode(t *testing.T) {
	f, id := runningWithFile(t)

	got, err := f.StatPath(context.Background(), id, "/app/main.go")
	if err != nil {
		t.Fatalf("StatPath() error = %v", err)
	}
	if got.Name != "main.go" {
		t.Errorf("Name = %q, want main.go", got.Name)
	}
	if got.Size != int64(len("package main")) {
		t.Errorf("Size = %d, want %d", got.Size, len("package main"))
	}
	if got.IsDir {
		t.Error("IsDir = true, want false")
	}
	if got.Mode.Perm() != 0o644 {
		t.Errorf("Mode = %v, want 0644", got.Mode.Perm())
	}
}

func TestStatPathOnDirectory(t *testing.T) {
	f, id := runningWithFile(t)

	got, err := f.StatPath(context.Background(), id, "/app")
	if err != nil {
		t.Fatalf("StatPath() error = %v", err)
	}
	if !got.IsDir {
		t.Error("IsDir = false, want true for /app")
	}
}

func TestStatPathMissingReturnsErrPathNotFound(t *testing.T) {
	f, id := runningWithFile(t)

	if _, err := f.StatPath(context.Background(), id, "/nope"); !errors.Is(err, runtime.ErrPathNotFound) {
		t.Errorf("StatPath() error = %v, want ErrPathNotFound", err)
	}
}

func TestStatPathUnknownContainerReturnsErrNotFound(t *testing.T) {
	f := New()
	if _, err := f.StatPath(context.Background(), "ghost", "/app"); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("StatPath() error = %v, want ErrNotFound", err)
	}
}

func TestStatPathWorksOnStoppedContainer(t *testing.T) {
	f, id := runningWithFile(t)
	if err := f.Stop(context.Background(), id, 0); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if _, err := f.StatPath(context.Background(), id, "/app/main.go"); err != nil {
		t.Errorf("StatPath() on stopped container error = %v, want nil", err)
	}
}

func TestReadArchiveIsRootedAtTheLastPathElement(t *testing.T) {
	f, id := runningWithFile(t)

	rc, err := f.ReadArchive(context.Background(), id, "/app/main.go")
	if err != nil {
		t.Fatalf("ReadArchive() error = %v", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	h, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next() error = %v", err)
	}
	if h.Name != "main.go" {
		t.Errorf("entry name = %q, want main.go", h.Name)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != "package main" {
		t.Errorf("body = %q, want package main", body)
	}
	if _, err := tr.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("second Next() = %v, want io.EOF", err)
	}
}

func TestReadArchiveOfDirectoryIsRecursive(t *testing.T) {
	f, id := runningWithFile(t)
	f.Seed(id, "/app/src/deep.go", 0o644, []byte("x"))

	rc, err := f.ReadArchive(context.Background(), id, "/app")
	if err != nil {
		t.Fatalf("ReadArchive() error = %v", err)
	}
	defer rc.Close()

	names := map[string]bool{}
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next() error = %v", err)
		}
		names[h.Name] = true
	}
	for _, want := range []string{"app/", "app/main.go", "app/src/", "app/src/deep.go"} {
		if !names[want] {
			t.Errorf("missing entry %q; got %v", want, names)
		}
	}
}

func TestWriteArchiveRequiresAnExistingDestination(t *testing.T) {
	f, id := runningWithFile(t)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "new.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte("y"))
	tw.Close()

	err := f.WriteArchive(context.Background(), id, "/missing", bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, runtime.ErrPathNotFound) {
		t.Errorf("WriteArchive() into missing dir = %v, want ErrPathNotFound", err)
	}
}

func TestWriteArchiveExtractsAndCreatesTarDirectories(t *testing.T) {
	f, id := runningWithFile(t)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "sub/", Mode: 0o755, Typeflag: tar.TypeDir})
	tw.WriteHeader(&tar.Header{Name: "sub/f.txt", Mode: 0o600, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.Close()

	if err := f.WriteArchive(context.Background(), id, "/app", bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}

	got, err := f.StatPath(context.Background(), id, "/app/sub/f.txt")
	if err != nil {
		t.Fatalf("StatPath() after extract error = %v", err)
	}
	if got.Size != 2 {
		t.Errorf("Size = %d, want 2", got.Size)
	}
	if got.Mode.Perm() != 0o600 {
		t.Errorf("Mode = %v, want 0600", got.Mode.Perm())
	}
	if _, err := f.StatPath(context.Background(), id, "/app/sub"); err != nil {
		t.Errorf("StatPath(/app/sub) error = %v, want the directory to exist", err)
	}
}

func TestWriteArchiveOverwritesWithoutRemovingSiblings(t *testing.T) {
	f, id := runningWithFile(t)
	f.Seed(id, "/app/keep.txt", 0o644, []byte("keep"))

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "main.go", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	tw.Write([]byte("new"))
	tw.Close()

	if err := f.WriteArchive(context.Background(), id, "/app", bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}

	if _, err := f.StatPath(context.Background(), id, "/app/keep.txt"); err != nil {
		t.Errorf("sibling was removed: StatPath(/app/keep.txt) = %v", err)
	}
	rc, _ := f.ReadArchive(context.Background(), id, "/app/main.go")
	defer rc.Close()
	tr := tar.NewReader(rc)
	tr.Next()
	body, _ := io.ReadAll(tr)
	if string(body) != "new" {
		t.Errorf("body = %q, want new", body)
	}
}
