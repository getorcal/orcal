package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/internal/files"
)

type cpEndpoint struct {
	ref    string
	path   string
	remote bool
}

func parseCPArg(arg string) cpEndpoint {
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return cpEndpoint{path: arg}
	}
	if isWindowsDrivePath(arg) {
		return cpEndpoint{path: arg}
	}
	if ref, path, found := strings.Cut(arg, ":"); found {
		return cpEndpoint{ref: ref, path: path, remote: true}
	}
	return cpEndpoint{path: arg}
}

func isWindowsDrivePath(arg string) bool {
	if len(arg) < 3 || arg[1] != ':' {
		return false
	}
	c := arg[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return isLetter && (arg[2] == '\\' || arg[2] == '/')
}

func (a *app) cpCmd() *cobra.Command {
	var recursive bool
	cmd := &cobra.Command{
		Use:   "cp <src> <dst>",
		Short: "Copy files between the local machine and a sandbox",
		Args:  cobra.ExactArgs(2),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			return a.runCP(cmd.Context(), args[0], args[1], recursive)
		}),
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "copy directories recursively")
	return cmd
}

func (a *app) runCP(ctx context.Context, srcArg, dstArg string, recursive bool) error {
	src := parseCPArg(srcArg)
	dst := parseCPArg(dstArg)

	switch {
	case !src.remote && !dst.remote:
		return fmt.Errorf("%w: cp requires exactly one of <src> and <dst> to be a sandbox ref in ref:path form, got zero", ErrUsage)
	case src.remote && dst.remote:
		return fmt.Errorf("%w: cp requires exactly one of <src> and <dst> to be a sandbox ref in ref:path form, got two", ErrUsage)
	case src.remote:
		return a.cpDownload(ctx, src, dst.path, recursive)
	default:
		return a.cpUpload(ctx, src.path, dst, recursive)
	}
}

func (a *app) cpUpload(ctx context.Context, localSrc string, dst cpEndpoint, recursive bool) error {
	info, err := os.Stat(localSrc)
	if err != nil {
		return err
	}
	if info.IsDir() && !recursive {
		return fmt.Errorf("%w: %s is a directory; use -r to copy directories", ErrUsage, localSrc)
	}
	if recursive {
		return a.uploadTree(ctx, localSrc, dst)
	}

	f, err := os.Open(localSrc)
	if err != nil {
		return err
	}
	defer f.Close()
	return a.client.WriteFile(ctx, dst.ref, dst.path, f)
}

func (a *app) uploadTree(ctx context.Context, localSrc string, dst cpEndpoint) error {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(buildLocalArchive(pw, localSrc))
	}()
	return a.client.UploadArchive(ctx, dst.ref, dst.path, pr)
}

func buildLocalArchive(w io.Writer, srcPath string) error {
	tw := tar.NewWriter(w)
	base := filepath.Base(srcPath)
	err := filepath.WalkDir(srcPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcPath, p)
		if err != nil {
			return err
		}
		name := base
		if rel != "." {
			name = base + "/" + filepath.ToSlash(rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return addTarEntry(tw, p, name, info)
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

func addTarEntry(tw *tar.Writer, fullPath, name string, info fs.FileInfo) error {
	var link string
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(fullPath)
		if err != nil {
			return err
		}
		link = target
	}
	h, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	h.Name = name
	if info.IsDir() {
		h.Name += "/"
	}
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		f, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) cpDownload(ctx context.Context, src cpEndpoint, localDst string, recursive bool) error {
	if recursive {
		return a.downloadTree(ctx, src, localDst)
	}

	rc, err := a.client.ReadFile(ctx, src.ref, src.path)
	if err != nil {
		return err
	}
	defer rc.Close()

	f, err := os.Create(localDst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, rc)
	return err
}

func (a *app) downloadTree(ctx context.Context, src cpEndpoint, localDst string) error {
	rc, err := a.client.DownloadArchive(ctx, src.ref, src.path)
	if err != nil {
		return err
	}
	defer rc.Close()

	stripRoot := true
	if info, err := os.Stat(localDst); err == nil && info.IsDir() {
		stripRoot = false
	}
	return extractArchive(rc, localDst, stripRoot)
}

func extractArchive(rc io.Reader, destRoot string, stripRoot bool) error {
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := files.SanitizeEntry(h, destRoot); err != nil {
			return err
		}

		name := h.Name
		if stripRoot {
			name = stripFirstComponent(name)
			if name == "" && h.Typeflag == tar.TypeDir {
				continue
			}
		}
		if err := extractEntry(tr, h, destRoot, name); err != nil {
			return err
		}
	}
}

func stripFirstComponent(name string) string {
	name = strings.TrimSuffix(name, "/")
	_, rest, found := strings.Cut(name, "/")
	if !found {
		return ""
	}
	return rest
}

func extractEntry(tr *tar.Reader, h *tar.Header, destRoot, name string) error {
	if name == "" {
		name = "."
	}
	target := filepath.Join(destRoot, filepath.FromSlash(name))

	switch h.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(h.Mode))
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		os.Remove(target)
		return os.Symlink(h.Linkname, target)
	case tar.TypeLink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		os.Remove(target)
		return os.Link(filepath.Join(destRoot, filepath.FromSlash(h.Linkname)), target)
	default:
		return fmt.Errorf("orcal: unsupported tar entry type for %s", h.Name)
	}
}
