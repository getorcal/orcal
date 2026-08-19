package files

import (
	"archive/tar"
	"errors"
	"testing"
)

func TestValidatePathAcceptsAbsoluteCleanPaths(t *testing.T) {
	for _, in := range []string{"/app", "/app/main.go", "/", "/etc/nginx/nginx.conf"} {
		got, err := ValidatePath(in)
		if err != nil {
			t.Errorf("ValidatePath(%q) error = %v, want nil", in, err)
		}
		if got != in {
			t.Errorf("ValidatePath(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestValidatePathCleansRedundancy(t *testing.T) {
	got, err := ValidatePath("/app//src/./main.go")
	if err != nil {
		t.Fatalf("ValidatePath() error = %v", err)
	}
	if got != "/app/src/main.go" {
		t.Errorf("ValidatePath() = %q, want /app/src/main.go", got)
	}
}

func TestValidatePathRejectsRelativeAndTraversal(t *testing.T) {
	for _, in := range []string{"", "app/main.go", "./main.go", "/app/../../etc/passwd", "../etc/passwd"} {
		if _, err := ValidatePath(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ValidatePath(%q) error = %v, want ErrInvalidPath", in, err)
		}
	}
}

func TestSanitizeEntryAcceptsRegularFilesAndDirs(t *testing.T) {
	for _, h := range []*tar.Header{
		{Name: "main.go", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "src/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "src/deep/f.txt", Typeflag: tar.TypeReg, Mode: 0o600},
	} {
		if err := SanitizeEntry(h, "/app"); err != nil {
			t.Errorf("SanitizeEntry(%q) error = %v, want nil", h.Name, err)
		}
	}
}

func TestSanitizeEntryRejectsEscapingNames(t *testing.T) {
	for _, name := range []string{"../escape.txt", "src/../../escape.txt", "/absolute.txt", "/etc/passwd"} {
		h := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}
		if err := SanitizeEntry(h, "/app"); !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("SanitizeEntry(%q) error = %v, want ErrUnsafeEntry", name, err)
		}
	}
}

func TestSanitizeEntryRejectsEscapingLinkTargets(t *testing.T) {
	cases := []*tar.Header{
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "../outside"},
	}
	for _, h := range cases {
		if err := SanitizeEntry(h, "/app"); !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("SanitizeEntry(link -> %q) error = %v, want ErrUnsafeEntry", h.Linkname, err)
		}
	}
}

func TestSanitizeEntryAcceptsInTreeSymlink(t *testing.T) {
	h := &tar.Header{Name: "src/link", Typeflag: tar.TypeSymlink, Linkname: "main.go"}
	if err := SanitizeEntry(h, "/app"); err != nil {
		t.Errorf("SanitizeEntry(in-tree symlink) error = %v, want nil", err)
	}
}

func TestSanitizeEntryRejectsNestedHardlinkEscape(t *testing.T) {
	h := &tar.Header{Name: "a/hard", Typeflag: tar.TypeLink, Linkname: "../etc/passwd"}
	if err := SanitizeEntry(h, "/app"); !errors.Is(err, ErrUnsafeEntry) {
		t.Errorf("SanitizeEntry(nested hardlink escaping) = %v, want ErrUnsafeEntry", err)
	}
}

func TestSanitizeEntryAcceptsRootRelativeHardlink(t *testing.T) {
	h := &tar.Header{Name: "a/hard", Typeflag: tar.TypeLink, Linkname: "a/main.go"}
	if err := SanitizeEntry(h, "/app"); err != nil {
		t.Errorf("SanitizeEntry(in-archive hardlink) = %v, want nil", err)
	}
}

func TestSanitizeEntryAcceptsNestedSymlinkAcrossDirectories(t *testing.T) {
	h := &tar.Header{Name: "a/b/c/link", Typeflag: tar.TypeSymlink, Linkname: "../../d"}
	if err := SanitizeEntry(h, "/app"); err != nil {
		t.Errorf("SanitizeEntry(in-tree nested symlink) = %v, want nil", err)
	}
}

func TestSanitizeEntryNormalizesName(t *testing.T) {
	h := &tar.Header{Name: "./a//b/../c.txt", Typeflag: tar.TypeReg, Mode: 0o644}
	if err := SanitizeEntry(h, "/app"); err != nil {
		t.Fatalf("SanitizeEntry() error = %v", err)
	}
	if h.Name != "a/c.txt" {
		t.Errorf("Name = %q, want %q", h.Name, "a/c.txt")
	}

	dir := &tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755}
	if err := SanitizeEntry(dir, "/app"); err != nil {
		t.Fatalf("SanitizeEntry() error = %v", err)
	}
	if dir.Name != "sub/" {
		t.Errorf("Name = %q, want %q", dir.Name, "sub/")
	}
}

func TestSanitizeEntryRejectsSpecialFileTypes(t *testing.T) {
	for _, tf := range []byte{tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
		h := &tar.Header{Name: "special", Typeflag: tf}
		if err := SanitizeEntry(h, "/app"); !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("SanitizeEntry(typeflag %v) error = %v, want ErrUnsafeEntry", tf, err)
		}
	}
}

func TestSanitizeEntryStripsSetuidAndSetgid(t *testing.T) {
	h := &tar.Header{Name: "tool", Typeflag: tar.TypeReg, Mode: 0o6755}
	if err := SanitizeEntry(h, "/app"); err != nil {
		t.Fatalf("SanitizeEntry() error = %v", err)
	}
	if h.Mode&(tarModeSetuid|tarModeSetgid) != 0 {
		t.Errorf("Mode = %#o, want setuid and setgid cleared", h.Mode)
	}
	if h.Mode&0o777 != 0o755 {
		t.Errorf("Mode = %#o, want 0755 preserved", h.Mode)
	}
}
