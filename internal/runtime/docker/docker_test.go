package docker

import (
	"errors"
	"testing"

	"github.com/docker/docker/errdefs"
	"github.com/getorcal/orcal/internal/runtime"
)

type opaqueDockerError struct{ msg string }

func (e opaqueDockerError) Error() string { return e.msg }

func TestTranslateMapsInvalidParameterToErrInvalidSpec(t *testing.T) {
	err := errdefs.InvalidParameter(errors.New("invalid image reference"))
	got := translate(err)
	if !errors.Is(got, runtime.ErrInvalidSpec) {
		t.Errorf("translate(%v) = %v, want wraps ErrInvalidSpec", err, got)
	}
}

func TestTranslateDoesNotLeakUnclassifiedDockerErrorType(t *testing.T) {
	original := opaqueDockerError{msg: "some daemon internal failure"}
	got := translate(original)

	var target opaqueDockerError
	if errors.As(got, &target) {
		t.Errorf("translate(%v) = %v, opaqueDockerError type leaked via errors.As", original, got)
	}
}

func TestTranslateMapsImageInUseToErrConflict(t *testing.T) {
	err := translate(errdefs.Conflict(errors.New("image is being used by running container")))
	if !errors.Is(err, runtime.ErrConflict) {
		t.Errorf("translate() = %v, want ErrConflict", err)
	}
}

func TestInspectReportsPausedDistinctly(t *testing.T) {
	got := containerStateFrom(true, true)
	if got != runtime.ContainerPaused {
		t.Errorf("running+paused = %s, want paused", got)
	}
	if s := containerStateFrom(true, false); s != runtime.ContainerRunning {
		t.Errorf("running = %s, want running", s)
	}
	if s := containerStateFrom(false, false); s != runtime.ContainerStopped {
		t.Errorf("stopped = %s, want stopped", s)
	}
}
