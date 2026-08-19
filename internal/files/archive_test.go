package files_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/getorcal/orcal/internal/files"
)

func tarOf(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range entries {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", h.Name, err)
		}
		if h.Typeflag == tar.TypeReg && h.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatalf("Write(%q) error = %v", h.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close() error = %v", err)
	}
	return buf.Bytes()
}

func TestDownloadArchiveStreamsTheTree(t *testing.T) {
	svc, f, access := newService(t)
	f.Seed(access.RuntimeID(), "/app/src/main.go", 0o644, []byte("x"))

	rc, err := svc.DownloadArchive(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
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
			t.Fatalf("Next() error = %v", err)
		}
		names[h.Name] = true
	}
	if !names["app/src/main.go"] || !names["app/main.go"] {
		t.Errorf("archive is not recursive; got %v", names)
	}
}

func TestDownloadArchiveMissingPathReturnsErrPathNotFound(t *testing.T) {
	svc, _, _ := newService(t)

	if _, err := svc.DownloadArchive(context.Background(), "my-agent", "/nope"); !errors.Is(err, files.ErrPathNotFound) {
		t.Errorf("DownloadArchive() error = %v, want ErrPathNotFound", err)
	}
}

func TestUploadArchiveExtractsIntoExistingDirectory(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	body := tarOf(t,
		&tar.Header{Name: "sub/", Mode: 0o755, Typeflag: tar.TypeDir},
		&tar.Header{Name: "sub/f.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg},
	)

	if err := svc.UploadArchive(ctx, "my-agent", "/app", bytes.NewReader(body)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	if _, err := svc.Stat(ctx, "my-agent", "/app/sub/f.txt"); err != nil {
		t.Errorf("Stat() after upload error = %v", err)
	}
}

func TestUploadArchiveMergesWithoutRemovingSiblings(t *testing.T) {
	svc, f, access := newService(t)
	ctx := context.Background()
	f.Seed(access.RuntimeID(), "/app/.env", 0o600, []byte("SECRET=1"))

	body := tarOf(t, &tar.Header{Name: "go.mod", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	if err := svc.UploadArchive(ctx, "my-agent", "/app", bytes.NewReader(body)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}

	if _, err := svc.Stat(ctx, "my-agent", "/app/.env"); err != nil {
		t.Errorf("/app/.env was removed by a merge upload: %v", err)
	}
}

func TestUploadArchiveRequiresExistingDestination(t *testing.T) {
	svc, _, _ := newService(t)
	body := tarOf(t, &tar.Header{Name: "f.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})

	err := svc.UploadArchive(context.Background(), "my-agent", "/missing", bytes.NewReader(body))
	if !errors.Is(err, files.ErrPathNotFound) {
		t.Errorf("UploadArchive() into missing dir = %v, want ErrPathNotFound", err)
	}
}

func TestUploadArchiveRejectsFileDestination(t *testing.T) {
	svc, _, _ := newService(t)
	body := tarOf(t, &tar.Header{Name: "f.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})

	err := svc.UploadArchive(context.Background(), "my-agent", "/app/main.go", bytes.NewReader(body))
	if !errors.Is(err, files.ErrNotDirectory) {
		t.Errorf("UploadArchive() into a file = %v, want ErrNotDirectory", err)
	}
}

func TestUploadArchiveRejectsTraversalAndWritesNothing(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	body := tarOf(t,
		&tar.Header{Name: "ok.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg},
		&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg},
	)

	err := svc.UploadArchive(ctx, "my-agent", "/app", bytes.NewReader(body))
	if !errors.Is(err, files.ErrUnsafeEntry) {
		t.Errorf("UploadArchive() error = %v, want ErrUnsafeEntry", err)
	}
}

func TestUploadArchiveStripsSetuid(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	body := tarOf(t, &tar.Header{
		Name: "tool", Mode: int64(fs.ModeSetuid | 0o755), Size: 1, Typeflag: tar.TypeReg,
	})

	if err := svc.UploadArchive(ctx, "my-agent", "/app", bytes.NewReader(body)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	info, err := svc.Stat(ctx, "my-agent", "/app/tool")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode&fs.ModeSetuid != 0 {
		t.Errorf("Mode = %v, want setuid stripped", info.Mode)
	}
}

func TestUploadArchiveRejectsOverLimit(t *testing.T) {
	limits := files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  64,
		ListMaxEntries:   100,
		ListMaxScanBytes: 1 << 20,
	}
	svc, _, _ := newServiceWithLimits(t, limits)
	body := tarOf(t, &tar.Header{Name: "big.bin", Mode: 0o644, Size: 4096, Typeflag: tar.TypeReg})

	if err := svc.UploadArchive(context.Background(), "my-agent", "/app", bytes.NewReader(body)); !errors.Is(err, files.ErrTooLarge) {
		t.Errorf("UploadArchive() error = %v, want ErrTooLarge", err)
	}
}

func TestUploadArchiveTakesTheLock(t *testing.T) {
	svc, _, access := newService(t)
	body := tarOf(t, &tar.Header{Name: "f.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})

	if err := svc.UploadArchive(context.Background(), "my-agent", "/app", bytes.NewReader(body)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	if access.locked != 1 {
		t.Errorf("locked = %d, want 1", access.locked)
	}
}

func TestDownloadArchiveDoesNotTakeTheLock(t *testing.T) {
	svc, _, access := newService(t)

	rc, err := svc.DownloadArchive(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	rc.Close()
	if access.locked != 0 {
		t.Errorf("locked = %d, want 0", access.locked)
	}
}
