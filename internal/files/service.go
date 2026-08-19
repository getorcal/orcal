package files

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/getorcal/orcal/internal/runtime"
)

type SandboxAccess interface {
	RuntimeIDFor(ctx context.Context, ref string) (string, error)
	WithLockedRuntimeID(ctx context.Context, ref string, fn func(runtimeID string) error) error
}

type Limits struct {
	FileMaxBytes     int64
	ArchiveMaxBytes  int64
	ListMaxEntries   int
	ListMaxScanBytes int64
}

type Service struct {
	sandboxes SandboxAccess
	rt        runtime.Runtime
	limits    Limits
}

func NewService(sandboxes SandboxAccess, rt runtime.Runtime, limits Limits) *Service {
	return &Service{sandboxes: sandboxes, rt: rt, limits: limits}
}

func (s *Service) Stat(ctx context.Context, ref, p string) (runtime.FileInfo, error) {
	cleaned, err := ValidatePath(p)
	if err != nil {
		return runtime.FileInfo{}, err
	}
	id, err := s.sandboxes.RuntimeIDFor(ctx, ref)
	if err != nil {
		return runtime.FileInfo{}, err
	}
	return s.stat(ctx, id, cleaned)
}

func (s *Service) stat(ctx context.Context, runtimeID, cleaned string) (runtime.FileInfo, error) {
	info, err := s.rt.StatPath(ctx, runtimeID, cleaned)
	if errors.Is(err, runtime.ErrPathNotFound) {
		return runtime.FileInfo{}, fmt.Errorf("%w: %s", ErrPathNotFound, cleaned)
	}
	if err != nil {
		return runtime.FileInfo{}, err
	}
	return info, nil
}

type entryReader struct {
	io.Reader
	closer io.Closer
}

func (e entryReader) Close() error { return e.closer.Close() }

func (s *Service) Read(ctx context.Context, ref, p string) (io.ReadCloser, runtime.FileInfo, error) {
	cleaned, err := ValidatePath(p)
	if err != nil {
		return nil, runtime.FileInfo{}, err
	}
	id, err := s.sandboxes.RuntimeIDFor(ctx, ref)
	if err != nil {
		return nil, runtime.FileInfo{}, err
	}

	info, err := s.stat(ctx, id, cleaned)
	if err != nil {
		return nil, runtime.FileInfo{}, err
	}
	if info.IsDir {
		return nil, runtime.FileInfo{}, fmt.Errorf("%w: %s is a directory", ErrNotRegular, cleaned)
	}
	if !info.Mode.IsRegular() {
		return nil, runtime.FileInfo{}, fmt.Errorf("%w: %s", ErrNotRegular, cleaned)
	}

	rc, err := s.rt.ReadArchive(ctx, id, cleaned)
	if err != nil {
		if errors.Is(err, runtime.ErrPathNotFound) {
			return nil, runtime.FileInfo{}, fmt.Errorf("%w: %s", ErrPathNotFound, cleaned)
		}
		return nil, runtime.FileInfo{}, err
	}

	tr := tar.NewReader(rc)
	h, err := tr.Next()
	if err != nil {
		rc.Close()
		return nil, runtime.FileInfo{}, fmt.Errorf("files: read archive header: %w", err)
	}
	if h.Typeflag != tar.TypeReg {
		rc.Close()
		return nil, runtime.FileInfo{}, fmt.Errorf("%w: %s", ErrNotRegular, cleaned)
	}
	return entryReader{Reader: tr, closer: rc}, info, nil
}
