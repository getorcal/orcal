package files

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/getorcal/orcal/internal/runtime"
)

func (s *Service) Write(ctx context.Context, ref, p string, body io.Reader) error {
	cleaned, err := ValidatePath(p)
	if err != nil {
		return err
	}
	if cleaned == "/" {
		return fmt.Errorf("%w: cannot write to the filesystem root", ErrInvalidPath)
	}

	return s.sandboxes.WithLockedRuntimeID(ctx, ref, func(runtimeID string) error {
		limited := io.LimitReader(body, s.limits.FileMaxBytes+1)
		buf, err := io.ReadAll(limited)
		if err != nil {
			return fmt.Errorf("files: read body: %w", err)
		}
		if int64(len(buf)) > s.limits.FileMaxBytes {
			return fmt.Errorf("%w: file exceeds %d bytes", ErrTooLarge, s.limits.FileMaxBytes)
		}

		targetInfo, err := s.rt.StatPath(ctx, runtimeID, cleaned)
		if err == nil {
			if targetInfo.IsDir {
				return fmt.Errorf("%w: %s is a directory", ErrNotRegular, cleaned)
			}
			if !targetInfo.Mode.IsRegular() {
				return fmt.Errorf("%w: %s", ErrNotRegular, cleaned)
			}
		} else if !errors.Is(err, runtime.ErrPathNotFound) {
			return err
		}

		anchor, missing, err := s.deepestExistingAncestor(ctx, runtimeID, cleaned)
		if err != nil {
			return err
		}
		archive, err := buildFileTar(missing, path.Base(cleaned), buf)
		if err != nil {
			return err
		}
		return s.rt.WriteArchive(ctx, runtimeID, anchor, bytes.NewReader(archive))
	})
}

func (s *Service) deepestExistingAncestor(ctx context.Context, runtimeID, target string) (string, []string, error) {
	var missing []string
	dir := path.Dir(target)
	for {
		info, err := s.rt.StatPath(ctx, runtimeID, dir)
		if err == nil {
			if !info.IsDir {
				return "", nil, fmt.Errorf("%w: %s is not a directory", ErrNotDirectory, dir)
			}
			return dir, missing, nil
		}
		if !errors.Is(err, runtime.ErrPathNotFound) {
			return "", nil, err
		}
		if dir == "/" {
			return "", nil, fmt.Errorf("%w: filesystem root is missing", ErrPathNotFound)
		}
		missing = append([]string{path.Base(dir)}, missing...)
		dir = path.Dir(dir)
	}
}

func buildFileTar(missingDirs []string, name string, body []byte) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	prefix := ""
	for _, d := range missingDirs {
		prefix = path.Join(prefix, d)
		if err := tw.WriteHeader(&tar.Header{
			Name:     prefix + "/",
			Mode:     0o755,
			Typeflag: tar.TypeDir,
		}); err != nil {
			return nil, fmt.Errorf("files: write dir header: %w", err)
		}
	}

	entry := name
	if prefix != "" {
		entry = path.Join(prefix, name)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     entry,
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return nil, fmt.Errorf("files: write file header: %w", err)
	}
	if _, err := tw.Write(body); err != nil {
		return nil, fmt.Errorf("files: write file body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("files: close tar: %w", err)
	}
	return buf.Bytes(), nil
}
