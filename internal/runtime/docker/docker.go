package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

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

type apiClient interface {
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (image.InspectResponse, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error)
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
	ContainerPause(ctx context.Context, containerID string) error
	ContainerUnpause(ctx context.Context, containerID string) error
	ContainerCommit(ctx context.Context, containerID string, options container.CommitOptions) (container.CommitResponse, error)
	ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error)
	ContainerStatPath(ctx context.Context, containerID, path string) (container.PathStat, error)
	CopyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, container.PathStat, error)
	CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error
	Info(ctx context.Context) (system.Info, error)
}

type Docker struct {
	cli apiClient
}

func New(host string) (*Docker, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", runtime.ErrUnavailable, err)
	}
	return &Docker{cli: cli}, nil
}

func (d *Docker) ResolveRuntime(ctx context.Context, configured string) (string, error) {
	info, err := d.cli.Info(ctx)
	if err != nil {
		return "", translate(err)
	}
	if configured == "" {
		return info.DefaultRuntime, nil
	}
	if _, ok := info.Runtimes[configured]; !ok {
		available := make([]string, 0, len(info.Runtimes))
		for name := range info.Runtimes {
			available = append(available, name)
		}
		sort.Strings(available)
		return "", fmt.Errorf("%w: runtime %q is not registered with this daemon; available: %s",
			runtime.ErrInvalidSpec, configured, strings.Join(available, ", "))
	}
	return configured, nil
}

func (d *Docker) EnsureNetwork(ctx context.Context, name string, internal bool) error {
	existing, err := d.cli.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		if existing.Internal != internal {
			return fmt.Errorf("%w: network %q exists with internal=%t, but internal=%t was requested",
				runtime.ErrInvalidSpec, name, existing.Internal, internal)
		}
		return nil
	}
	if !cerrdefs.IsNotFound(err) {
		return translate(err)
	}
	_, err = d.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: internal,
		Options: map[string]string{
			"com.docker.network.bridge.enable_icc": "false",
		},
	})
	if err != nil && !cerrdefs.IsConflict(err) {
		return translate(err)
	}
	return nil
}

func (d *Docker) Create(ctx context.Context, spec runtime.CreateSpec) (string, error) {
	if err := d.ensureImage(ctx, spec.Image); err != nil {
		return "", err
	}
	created, err := d.cli.ContainerCreate(ctx, containerConfig(spec), hostConfig(spec), nil, nil, "")
	if err != nil {
		return "", translate(err)
	}
	return created.ID, nil
}

func (d *Docker) ensureImage(ctx context.Context, ref string) error {
	_, inspectErr := d.cli.ImageInspect(ctx, ref)
	if inspectErr == nil {
		return nil
	}
	if !cerrdefs.IsNotFound(inspectErr) {
		return translate(inspectErr)
	}
	reader, err := d.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return translate(err)
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return translate(err)
	}
	return nil
}

func (d *Docker) Start(ctx context.Context, id string) error {
	return translate(d.cli.ContainerStart(ctx, id, container.StartOptions{}))
}

func (d *Docker) Stop(ctx context.Context, id string, timeout time.Duration) error {
	seconds := int(timeout.Seconds())
	return translate(d.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &seconds}))
}

func (d *Docker) Destroy(ctx context.Context, id string) error {
	err := d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return translate(err)
}

func (d *Docker) Inspect(ctx context.Context, id string) (runtime.Status, error) {
	info, err := d.cli.ContainerInspect(ctx, id)
	if cerrdefs.IsNotFound(err) {
		return runtime.Status{State: runtime.ContainerGone}, nil
	}
	if err != nil {
		return runtime.Status{}, translate(err)
	}
	status := runtime.Status{State: runtime.ContainerStopped}
	if info.State != nil {
		status.State = containerStateFrom(info.State.Running, info.State.Paused)
	}
	if info.State != nil && !info.State.Running {
		code := info.State.ExitCode
		status.ExitCode = &code
	}
	return status, nil
}

// Docker reports State.Running == true for a paused container, so paused must be
// checked first or a frozen sandbox reads as healthy while every exec against it hangs.
func containerStateFrom(running, paused bool) runtime.ContainerState {
	switch {
	case running && paused:
		return runtime.ContainerPaused
	case running:
		return runtime.ContainerRunning
	default:
		return runtime.ContainerStopped
	}
}

func (d *Docker) Exec(ctx context.Context, id string, spec runtime.ExecSpec) (runtime.Session, error) {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	created, err := d.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          spec.Command,
		Env:          env,
		WorkingDir:   spec.WorkingDir,
		User:         spec.User,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  false,
		Tty:          false,
	})
	if err != nil {
		return nil, translate(err)
	}

	attached, err := d.cli.ContainerExecAttach(ctx, created.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, translate(err)
	}
	return newSession(d.cli, created.ID, attached), nil
}

func (d *Docker) InspectExec(ctx context.Context, execID string) (runtime.ExecStatus, error) {
	info, err := d.cli.ContainerExecInspect(ctx, execID)
	if cerrdefs.IsNotFound(err) {
		return runtime.ExecStatus{}, fmt.Errorf("%w: exec %s", runtime.ErrNotFound, execID)
	}
	if err != nil {
		return runtime.ExecStatus{}, translate(err)
	}
	status := runtime.ExecStatus{Running: info.Running}
	if !info.Running {
		code := info.ExitCode
		status.ExitCode = &code
	}
	return status, nil
}

func (d *Docker) Snapshot(ctx context.Context, id string) (runtime.SnapshotInfo, error) {
	info, err := d.cli.ContainerInspect(ctx, id)
	if cerrdefs.IsNotFound(err) {
		return runtime.SnapshotInfo{}, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	if err != nil {
		return runtime.SnapshotInfo{}, translate(err)
	}

	// Pausing a stopped container errors, and snapshotting a stopped sandbox is legitimate.
	wasRunning := info.State != nil && info.State.Running && !info.State.Paused
	if wasRunning {
		if err := d.cli.ContainerPause(ctx, id); err != nil {
			return runtime.SnapshotInfo{}, translate(err)
		}
		// WithoutCancel is load-bearing: if the caller's context was canceled mid-commit,
		// unpausing on that same context would fail and strand the container paused forever.
		// The unpause is also unconditional — a failed commit must not leave the sandbox frozen.
		defer func() {
			_ = d.cli.ContainerUnpause(context.WithoutCancel(ctx), id)
		}()
	}

	committed, err := d.cli.ContainerCommit(ctx, id, container.CommitOptions{})
	if err != nil {
		return runtime.SnapshotInfo{}, translate(err)
	}

	inspected, err := d.cli.ImageInspect(ctx, committed.ID)
	if err != nil {
		return runtime.SnapshotInfo{Ref: committed.ID}, nil
	}
	return runtime.SnapshotInfo{Ref: committed.ID, SizeBytes: inspected.Size}, nil
}

func (d *Docker) DeleteSnapshot(ctx context.Context, ref string) error {
	_, err := d.cli.ImageRemove(ctx, ref, image.RemoveOptions{})
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: snapshot %s", runtime.ErrNotFound, ref)
	}
	return translate(err)
}

func (d *Docker) Unpause(ctx context.Context, id string) error {
	err := d.cli.ContainerUnpause(ctx, id)
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	// Docker returns a conflict when the container is not paused — the state the caller wanted.
	if cerrdefs.IsConflict(err) {
		return nil
	}
	return translate(err)
}

func (d *Docker) DiskQuotaSupported(ctx context.Context) (bool, error) {
	info, err := d.cli.Info(ctx)
	if err != nil {
		return false, translate(err)
	}
	status := make([][2]string, 0, len(info.DriverStatus))
	for _, pair := range info.DriverStatus {
		if len(pair) == 2 {
			status = append(status, [2]string{pair[0], pair[1]})
		}
	}
	return diskQuotaSupported(info.Driver, status), nil
}

// The %v verbs are deliberate and errorlint is suppressed for this file because of them.
// translate is the boundary of the runtime abstraction: it maps Docker's errors onto orcal's
// own sentinels, and formatting the cause with %v rather than %w keeps Docker's concrete error
// types from escaping through errors.As into packages that must not know Docker exists.
// TestTranslateDoesNotLeakUnclassifiedDockerErrorType fails if this is "fixed" to %w.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case cerrdefs.IsNotFound(err):
		return fmt.Errorf("%w: %v", runtime.ErrNotFound, err)
	case cerrdefs.IsConflict(err):
		return fmt.Errorf("%w: %v", runtime.ErrConflict, err)
	case cerrdefs.IsInvalidArgument(err):
		return fmt.Errorf("%w: %v", runtime.ErrInvalidSpec, err)
	case client.IsErrConnectionFailed(err), cerrdefs.IsUnavailable(err), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", runtime.ErrUnavailable, err)
	default:
		return fmt.Errorf("runtime: docker: %v", err)
	}
}
