package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"

	"github.com/getorcal/orcal/internal/runtime"
)

func fileInfoFrom(st container.PathStat) runtime.FileInfo {
	return runtime.FileInfo{
		Name:       st.Name,
		LinkTarget: st.LinkTarget,
		Size:       st.Size,
		Mode:       st.Mode,
		ModTime:    st.Mtime,
		IsDir:      st.Mode.IsDir(),
	}
}

func translatePath(err error, p string) error {
	if err == nil {
		return nil
	}
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: %s", runtime.ErrPathNotFound, p)
	}
	return translate(err)
}

func isNoSuchContainer(err error, id string) bool {
	return cerrdefs.IsNotFound(err) && strings.Contains(err.Error(), "No such container")
}

func (d *Docker) StatPath(ctx context.Context, id, p string) (runtime.FileInfo, error) {
	st, err := d.cli.ContainerStatPath(ctx, id, p)
	if err != nil {
		if isNoSuchContainer(err, id) {
			return runtime.FileInfo{}, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
		}
		return runtime.FileInfo{}, translatePath(err, p)
	}
	return fileInfoFrom(st), nil
}

func (d *Docker) ReadArchive(ctx context.Context, id, p string) (io.ReadCloser, error) {
	rc, _, err := d.cli.CopyFromContainer(ctx, id, p)
	if err != nil {
		if isNoSuchContainer(err, id) {
			return nil, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
		}
		return nil, translatePath(err, p)
	}
	return rc, nil
}

func (d *Docker) WriteArchive(ctx context.Context, id, destDir string, r io.Reader) error {
	err := d.cli.CopyToContainer(ctx, id, destDir, r, container.CopyToContainerOptions{})
	if err != nil {
		if isNoSuchContainer(err, id) {
			return fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
		}
		return translatePath(err, destDir)
	}
	return nil
}
