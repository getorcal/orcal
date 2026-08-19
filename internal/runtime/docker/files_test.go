package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"

	"github.com/getorcal/orcal/internal/runtime"
)

func errNotFoundForTest() error {
	return fmt.Errorf("no such file: %w", cerrdefs.ErrNotFound)
}

func TestFileInfoFromPathStat(t *testing.T) {
	when := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	got := fileInfoFrom(container.PathStat{
		Name:       "main.go",
		Size:       12,
		Mode:       0o644,
		Mtime:      when,
		LinkTarget: "",
	})

	if got.Name != "main.go" || got.Size != 12 {
		t.Errorf("got %+v, want name main.go size 12", got)
	}
	if got.Mode.Perm() != 0o644 {
		t.Errorf("Mode = %v, want 0644", got.Mode.Perm())
	}
	if !got.ModTime.Equal(when) {
		t.Errorf("ModTime = %v, want %v", got.ModTime, when)
	}
	if got.IsDir {
		t.Error("IsDir = true, want false")
	}
}

func TestFileInfoFromPathStatDirectory(t *testing.T) {
	got := fileInfoFrom(container.PathStat{Name: "app", Mode: fs.ModeDir | 0o755})
	if !got.IsDir {
		t.Error("IsDir = false, want true when ModeDir is set")
	}
}

func TestTranslatePathMapsNotFoundToErrPathNotFound(t *testing.T) {
	err := translatePath(errNotFoundForTest(), "/app/missing")
	if !errors.Is(err, runtime.ErrPathNotFound) {
		t.Errorf("translatePath() = %v, want ErrPathNotFound", err)
	}
	if errors.Is(err, runtime.ErrNotFound) {
		t.Error("a missing path must not also satisfy ErrNotFound, or the API cannot tell it from a missing sandbox")
	}
}
