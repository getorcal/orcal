package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/getorcal/orcal/internal/runtime"
)

type opaqueDockerError struct{ msg string }

func (e opaqueDockerError) Error() string { return e.msg }

func TestTranslateMapsInvalidParameterToErrInvalidSpec(t *testing.T) {
	err := fmt.Errorf("invalid image reference: %w", cerrdefs.ErrInvalidArgument)
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
	err := translate(fmt.Errorf("image is being used by running container: %w", cerrdefs.ErrConflict))
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

type fakeInfo struct {
	Default   string
	Available []string
}

func newFakeDocker(t *testing.T, fi fakeInfo) *Docker {
	t.Helper()
	runtimes := make(map[string]system.RuntimeWithStatus, len(fi.Available))
	for _, name := range fi.Available {
		runtimes[name] = system.RuntimeWithStatus{}
	}
	return &Docker{cli: fakeDockerClient{
		info: system.Info{DefaultRuntime: fi.Default, Runtimes: runtimes},
	}}
}

type fakeDockerClient struct {
	info system.Info
}

func (f fakeDockerClient) Info(context.Context) (system.Info, error) { return f.info, nil }

func (f fakeDockerClient) NetworkInspect(context.Context, string, network.InspectOptions) (network.Inspect, error) {
	panic("fakeDockerClient: NetworkInspect not implemented")
}

func (f fakeDockerClient) NetworkCreate(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
	panic("fakeDockerClient: NetworkCreate not implemented")
}

func (f fakeDockerClient) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error) {
	panic("fakeDockerClient: ContainerCreate not implemented")
}

func (f fakeDockerClient) ImageInspect(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
	panic("fakeDockerClient: ImageInspect not implemented")
}

func (f fakeDockerClient) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	panic("fakeDockerClient: ImagePull not implemented")
}

func (f fakeDockerClient) ContainerStart(context.Context, string, container.StartOptions) error {
	panic("fakeDockerClient: ContainerStart not implemented")
}

func (f fakeDockerClient) ContainerStop(context.Context, string, container.StopOptions) error {
	panic("fakeDockerClient: ContainerStop not implemented")
}

func (f fakeDockerClient) ContainerRemove(context.Context, string, container.RemoveOptions) error {
	panic("fakeDockerClient: ContainerRemove not implemented")
}

func (f fakeDockerClient) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	panic("fakeDockerClient: ContainerInspect not implemented")
}

func (f fakeDockerClient) ContainerExecCreate(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
	panic("fakeDockerClient: ContainerExecCreate not implemented")
}

func (f fakeDockerClient) ContainerExecAttach(context.Context, string, container.ExecAttachOptions) (types.HijackedResponse, error) {
	panic("fakeDockerClient: ContainerExecAttach not implemented")
}

func (f fakeDockerClient) ContainerExecInspect(context.Context, string) (container.ExecInspect, error) {
	panic("fakeDockerClient: ContainerExecInspect not implemented")
}

func (f fakeDockerClient) ContainerPause(context.Context, string) error {
	panic("fakeDockerClient: ContainerPause not implemented")
}

func (f fakeDockerClient) ContainerUnpause(context.Context, string) error {
	panic("fakeDockerClient: ContainerUnpause not implemented")
}

func (f fakeDockerClient) ContainerCommit(context.Context, string, container.CommitOptions) (container.CommitResponse, error) {
	panic("fakeDockerClient: ContainerCommit not implemented")
}

func (f fakeDockerClient) ImageRemove(context.Context, string, image.RemoveOptions) ([]image.DeleteResponse, error) {
	panic("fakeDockerClient: ImageRemove not implemented")
}

func (f fakeDockerClient) ContainerStatPath(context.Context, string, string) (container.PathStat, error) {
	panic("fakeDockerClient: ContainerStatPath not implemented")
}

func (f fakeDockerClient) CopyFromContainer(context.Context, string, string) (io.ReadCloser, container.PathStat, error) {
	panic("fakeDockerClient: CopyFromContainer not implemented")
}

func (f fakeDockerClient) CopyToContainer(context.Context, string, string, io.Reader, container.CopyToContainerOptions) error {
	panic("fakeDockerClient: CopyToContainer not implemented")
}
