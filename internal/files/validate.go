package files

import (
	"archive/tar"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

func ValidatePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("%w: path is required", ErrInvalidPath)
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: path must be absolute", ErrInvalidPath)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", fmt.Errorf("%w: path escapes the filesystem root", ErrInvalidPath)
	}
	return path.Clean(p), nil
}

func SanitizeEntry(h *tar.Header, destDir string) error {
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeDir, tar.TypeSymlink, tar.TypeLink:
	default:
		return fmt.Errorf("%w: %s has unsupported type %v", ErrUnsafeEntry, h.Name, h.Typeflag)
	}

	if strings.HasPrefix(h.Name, "/") {
		return fmt.Errorf("%w: %s is absolute", ErrUnsafeEntry, h.Name)
	}
	rel := path.Clean(h.Name)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%w: %s escapes the destination", ErrUnsafeEntry, h.Name)
	}

	if h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeLink {
		if strings.HasPrefix(h.Linkname, "/") {
			return fmt.Errorf("%w: %s targets an absolute path", ErrUnsafeEntry, h.Name)
		}
		resolved := path.Clean(path.Join(path.Dir(rel), h.Linkname))
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return fmt.Errorf("%w: %s targets %s outside the destination", ErrUnsafeEntry, h.Name, h.Linkname)
		}
	}

	h.Mode = int64(fs.FileMode(h.Mode) & ^(fs.ModeSetuid | fs.ModeSetgid))
	h.Uid, h.Gid = 0, 0
	h.Uname, h.Gname = "", ""
	return nil
}
