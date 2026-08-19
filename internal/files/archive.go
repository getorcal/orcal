package files

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/getorcal/orcal/internal/runtime"
)

func (s *Service) DownloadArchive(ctx context.Context, ref, p string) (io.ReadCloser, error) {
	cleaned, err := ValidatePath(p)
	if err != nil {
		return nil, err
	}
	id, err := s.sandboxes.RuntimeIDFor(ctx, ref)
	if err != nil {
		return nil, err
	}
	if _, err := s.stat(ctx, id, cleaned); err != nil {
		return nil, err
	}

	rc, err := s.rt.ReadArchive(ctx, id, cleaned)
	if err != nil {
		if errors.Is(err, runtime.ErrPathNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrPathNotFound, cleaned)
		}
		return nil, err
	}
	return rc, nil
}

func (s *Service) UploadArchive(ctx context.Context, ref, destDir string, body io.Reader) error {
	cleaned, err := ValidatePath(destDir)
	if err != nil {
		return err
	}

	return s.sandboxes.WithLockedRuntimeID(ctx, ref, func(runtimeID string) error {
		info, err := s.stat(ctx, runtimeID, cleaned)
		if err != nil {
			return err
		}
		if !info.IsDir {
			return fmt.Errorf("%w: %s", ErrNotDirectory, cleaned)
		}

		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(sanitizeArchive(body, pw, cleaned, s.limits.ArchiveMaxBytes))
		}()

		writeErr := s.rt.WriteArchive(ctx, runtimeID, cleaned, pr)
		closeErr := pr.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	})
}

func sanitizeArchive(src io.Reader, dst io.Writer, destDir string, maxBytes int64) error {
	counted := &countingReader{r: src}
	tr := tar.NewReader(counted)
	tw := tar.NewWriter(dst)

	var written int64
	for {
		if counted.n > maxBytes {
			return fmt.Errorf("%w: archive exceeds %d bytes", ErrTooLarge, maxBytes)
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("files: read uploaded tar: %w", err)
		}
		if err := SanitizeEntry(h, destDir); err != nil {
			return err
		}

		written += h.Size
		if written > maxBytes {
			return fmt.Errorf("%w: archive exceeds %d bytes", ErrTooLarge, maxBytes)
		}
		if err := tw.WriteHeader(h); err != nil {
			return fmt.Errorf("files: write tar header: %w", err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tw, tr); err != nil {
				return fmt.Errorf("files: copy tar body: %w", err)
			}
		}
	}
	return tw.Close()
}
