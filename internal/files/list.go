package files

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/getorcal/orcal/internal/runtime"
)

type Listing struct {
	Items     []runtime.FileInfo
	Truncated bool
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	read, err := c.r.Read(p)
	c.n += int64(read)
	return read, err
}

func (s *Service) List(ctx context.Context, ref, p string) (Listing, error) {
	cleaned, err := ValidatePath(p)
	if err != nil {
		return Listing{}, err
	}
	id, err := s.sandboxes.RuntimeIDFor(ctx, ref)
	if err != nil {
		return Listing{}, err
	}

	info, err := s.stat(ctx, id, cleaned)
	if err != nil {
		return Listing{}, err
	}
	if !info.IsDir {
		return Listing{Items: []runtime.FileInfo{info}}, nil
	}

	rc, err := s.rt.ReadArchive(ctx, id, cleaned)
	if err != nil {
		if errors.Is(err, runtime.ErrPathNotFound) {
			return Listing{}, fmt.Errorf("%w: %s", ErrPathNotFound, cleaned)
		}
		return Listing{}, err
	}
	defer rc.Close()

	counted := &countingReader{r: rc}
	tr := tar.NewReader(counted)
	out := Listing{}

	for {
		if counted.n > s.limits.ListMaxScanBytes {
			out.Truncated = true
			break
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Listing{}, fmt.Errorf("files: scan archive: %w", err)
		}

		name := path.Clean(strings.TrimSuffix(h.Name, "/"))
		parts := strings.Split(name, "/")
		if len(parts) != 2 {
			continue
		}
		if len(out.Items) >= s.limits.ListMaxEntries {
			out.Truncated = true
			break
		}
		out.Items = append(out.Items, runtime.FileInfo{
			Name:       parts[1],
			LinkTarget: h.Linkname,
			Size:       h.Size,
			Mode:       fs.FileMode(h.Mode),
			ModTime:    h.ModTime,
			IsDir:      h.Typeflag == tar.TypeDir,
		})
	}
	return out, nil
}
