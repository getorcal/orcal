package fake

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
)

type fileNode struct {
	mode    fs.FileMode
	data    []byte
	isDir   bool
	modTime time.Time
}

func (f *Fake) Seed(id, p string, mode fs.FileMode, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return
	}
	if c.files == nil {
		c.files = map[string]*fileNode{"/": {mode: 0o755, isDir: true}}
	}
	p = path.Clean(p)
	for _, dir := range ancestors(p) {
		if _, exists := c.files[dir]; !exists {
			c.files[dir] = &fileNode{mode: 0o755, isDir: true, modTime: fakeModTime}
		}
	}
	c.files[p] = &fileNode{mode: mode, data: data, modTime: fakeModTime}
}

func (f *Fake) SeedDir(id, p string, mode fs.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return
	}
	if c.files == nil {
		c.files = map[string]*fileNode{"/": {mode: 0o755, isDir: true, modTime: fakeModTime}}
	}
	p = path.Clean(p)
	for _, dir := range ancestors(p) {
		if _, exists := c.files[dir]; !exists {
			c.files[dir] = &fileNode{mode: 0o755, isDir: true, modTime: fakeModTime}
		}
	}
	c.files[p] = &fileNode{mode: mode, isDir: true, modTime: fakeModTime}
}

var fakeModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ancestors(p string) []string {
	var out []string
	dir := path.Dir(path.Clean(p))
	for dir != "/" && dir != "." {
		out = append([]string{dir}, out...)
		dir = path.Dir(dir)
	}
	return append([]string{"/"}, out...)
}

func (f *Fake) fsFor(id string) (*container, map[string]*fileNode, error) {
	c, ok := f.containers[id]
	if !ok || c.state == runtime.ContainerGone {
		return nil, nil, fmt.Errorf("%w: container %s", runtime.ErrNotFound, id)
	}
	if c.files == nil {
		c.files = map[string]*fileNode{"/": {mode: 0o755, isDir: true, modTime: fakeModTime}}
	}
	return c, c.files, nil
}

func (f *Fake) StatPath(ctx context.Context, id, p string) (runtime.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, files, err := f.fsFor(id)
	if err != nil {
		return runtime.FileInfo{}, err
	}
	p = path.Clean(p)
	n, ok := files[p]
	if !ok {
		return runtime.FileInfo{}, fmt.Errorf("%w: %s", runtime.ErrPathNotFound, p)
	}
	return runtime.FileInfo{
		Name:    path.Base(p),
		Size:    int64(len(n.data)),
		Mode:    n.mode,
		ModTime: n.modTime,
		IsDir:   n.isDir,
	}, nil
}

func (f *Fake) ReadArchive(ctx context.Context, id, p string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, files, err := f.fsFor(id)
	if err != nil {
		return nil, err
	}
	p = path.Clean(p)
	root, ok := files[p]
	if !ok {
		return nil, fmt.Errorf("%w: %s", runtime.ErrPathNotFound, p)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	base := path.Base(p)

	write := func(name string, n *fileNode) error {
		h := &tar.Header{
			Name:     name,
			Mode:     int64(n.mode.Perm()),
			ModTime:  n.modTime,
			Typeflag: tar.TypeReg,
		}
		if n.isDir {
			h.Typeflag = tar.TypeDir
			h.Name = name + "/"
		} else {
			h.Size = int64(len(n.data))
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !n.isDir {
			if _, err := tw.Write(n.data); err != nil {
				return err
			}
		}
		return nil
	}

	if !root.isDir {
		if err := write(base, root); err != nil {
			return nil, err
		}
	} else {
		keys := make([]string, 0, len(files))
		for k := range files {
			if k == p || strings.HasPrefix(k, strings.TrimSuffix(p, "/")+"/") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			rel := strings.TrimPrefix(strings.TrimPrefix(k, p), "/")
			name := base
			if rel != "" {
				name = base + "/" + rel
			}
			if err := write(name, files[k]); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func (f *Fake) WriteArchive(ctx context.Context, id, destDir string, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, files, err := f.fsFor(id)
	if err != nil {
		return err
	}
	destDir = path.Clean(destDir)
	dest, ok := files[destDir]
	if !ok || !dest.isDir {
		return fmt.Errorf("%w: %s", runtime.ErrPathNotFound, destDir)
	}

	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("fake: read tar: %w", err)
		}
		target := path.Join(destDir, path.Clean(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			files[target] = &fileNode{mode: fs.FileMode(h.Mode).Perm(), isDir: true, modTime: h.ModTime}
		case tar.TypeReg:
			body, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("fake: read tar body: %w", err)
			}
			for _, dir := range ancestors(target) {
				if _, exists := files[dir]; !exists {
					files[dir] = &fileNode{mode: 0o755, isDir: true, modTime: h.ModTime}
				}
			}
			files[target] = &fileNode{mode: fs.FileMode(h.Mode).Perm(), data: body, modTime: h.ModTime}
		default:
			return fmt.Errorf("fake: unsupported tar entry type %v", h.Typeflag)
		}
	}
	return nil
}
