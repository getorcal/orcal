package files_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
)

type stubAccess struct {
	runtimeID string
	err       error
	locked    int
}

func (s *stubAccess) RuntimeIDFor(ctx context.Context, ref string) (string, error) {
	return s.runtimeID, s.err
}

func (s *stubAccess) WithLockedRuntimeID(ctx context.Context, ref string, fn func(string) error) error {
	if s.err != nil {
		return s.err
	}
	s.locked++
	return fn(s.runtimeID)
}

func (s *stubAccess) RuntimeID() string { return s.runtimeID }

func newServiceWithLimits(t *testing.T, limits files.Limits) (*files.Service, *fake.Fake, *stubAccess) {
	t.Helper()
	f := fake.New()
	ctx := context.Background()
	id, err := f.Create(ctx, runtime.CreateSpec{Image: "alpine"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := f.Start(ctx, id); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	f.Seed(id, "/app/main.go", 0o644, []byte("package main"))

	access := &stubAccess{runtimeID: id}
	return files.NewService(access, f, limits), f, access
}

func newService(t *testing.T) (*files.Service, *fake.Fake, *stubAccess) {
	t.Helper()
	limits := files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   100,
		ListMaxScanBytes: 1 << 20,
	}
	return newServiceWithLimits(t, limits)
}

func TestStatReturnsFileInfo(t *testing.T) {
	svc, _, _ := newService(t)

	got, err := svc.Stat(context.Background(), "my-agent", "/app/main.go")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got.Name != "main.go" || got.Size != 12 || got.IsDir {
		t.Errorf("got %+v, want main.go size 12 file", got)
	}
}

func TestStatRejectsRelativePath(t *testing.T) {
	svc, _, _ := newService(t)

	if _, err := svc.Stat(context.Background(), "my-agent", "app/main.go"); !errors.Is(err, files.ErrInvalidPath) {
		t.Errorf("Stat() error = %v, want ErrInvalidPath", err)
	}
}

func TestStatMissingPathReturnsFilesErrPathNotFound(t *testing.T) {
	svc, _, _ := newService(t)

	_, err := svc.Stat(context.Background(), "my-agent", "/nope")
	if !errors.Is(err, files.ErrPathNotFound) {
		t.Errorf("Stat() error = %v, want files.ErrPathNotFound", err)
	}
	if errors.Is(err, runtime.ErrNotFound) {
		t.Error("a missing path must not satisfy runtime.ErrNotFound, or the API reports it as a missing sandbox")
	}
}

func TestReadStreamsFileBody(t *testing.T) {
	svc, _, _ := newService(t)

	rc, info, err := svc.Read(context.Background(), "my-agent", "/app/main.go")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	defer rc.Close()

	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "package main" {
		t.Errorf("body = %q, want package main", body)
	}
	if info.Size != 12 {
		t.Errorf("info.Size = %d, want 12", info.Size)
	}
}

func TestReadRejectsDirectory(t *testing.T) {
	svc, _, _ := newService(t)

	if _, _, err := svc.Read(context.Background(), "my-agent", "/app"); !errors.Is(err, files.ErrNotRegular) {
		t.Errorf("Read(dir) error = %v, want ErrNotRegular", err)
	}
}

func TestReadPropagatesSandboxLookupError(t *testing.T) {
	svc, _, access := newService(t)
	boom := errors.New("no such sandbox")
	access.err = boom

	if _, _, err := svc.Read(context.Background(), "ghost", "/app/main.go"); !errors.Is(err, boom) {
		t.Errorf("Read() error = %v, want the lookup error", err)
	}
}

func TestReadDoesNotTakeTheLock(t *testing.T) {
	svc, _, access := newService(t)

	rc, _, err := svc.Read(context.Background(), "my-agent", "/app/main.go")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	rc.Close()

	if access.locked != 0 {
		t.Errorf("locked = %d, want 0; reads must not block snapshots", access.locked)
	}
}
