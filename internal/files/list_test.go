package files_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getorcal/orcal/internal/files"
)

func TestListReturnsOneLevelOnly(t *testing.T) {
	svc, f, access := newService(t)
	f.Seed(access.RuntimeID(), "/app/go.mod", 0o644, []byte("module x"))
	f.Seed(access.RuntimeID(), "/app/src/main.go", 0o644, []byte("x"))
	f.Seed(access.RuntimeID(), "/app/src/deep/inner.go", 0o644, []byte("y"))

	got, err := svc.List(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	names := map[string]bool{}
	for _, it := range got.Items {
		names[it.Name] = true
	}
	for _, want := range []string{"main.go", "go.mod", "src"} {
		if !names[want] {
			t.Errorf("missing %q in listing; got %v", want, names)
		}
	}
	if names["inner.go"] || names["deep"] {
		t.Errorf("listing is recursive; got %v", names)
	}
	if got.Truncated {
		t.Error("Truncated = true, want false for a small tree")
	}
}

func TestListMarksDirectories(t *testing.T) {
	svc, f, access := newService(t)
	f.Seed(access.RuntimeID(), "/app/src/main.go", 0o644, []byte("x"))

	got, err := svc.List(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var seen bool
	for _, it := range got.Items {
		if it.Name == "src" {
			seen = true
			if !it.IsDir {
				t.Error("src.IsDir = false, want true")
			}
		}
	}
	if !seen {
		t.Fatal("src not present in listing")
	}
}

func TestListOfARegularFileReturnsThatFile(t *testing.T) {
	svc, _, _ := newService(t)

	got, err := svc.List(context.Background(), "my-agent", "/app/main.go")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "main.go" {
		t.Errorf("got %+v, want a single main.go entry", got.Items)
	}
}

func TestListMissingPathReturnsErrPathNotFound(t *testing.T) {
	svc, _, _ := newService(t)

	if _, err := svc.List(context.Background(), "my-agent", "/nope"); !errors.Is(err, files.ErrPathNotFound) {
		t.Errorf("List() error = %v, want ErrPathNotFound", err)
	}
}

func TestListTruncatesAtEntryCap(t *testing.T) {
	limits := files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   3,
		ListMaxScanBytes: 1 << 20,
	}
	svc, f, access := newServiceWithLimits(t, limits)
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		f.Seed(access.RuntimeID(), "/app/"+n+".txt", 0o644, []byte("x"))
	}

	got, err := svc.List(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(got.Items))
	}
	if !got.Truncated {
		t.Error("Truncated = false; a capped listing must say so rather than look complete")
	}
}

func TestListTruncatesAtScanCap(t *testing.T) {
	limits := files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   100,
		ListMaxScanBytes: 1024,
	}
	svc, f, access := newServiceWithLimits(t, limits)
	f.Seed(access.RuntimeID(), "/app/big.bin", 0o644, make([]byte, 4096))
	f.Seed(access.RuntimeID(), "/app/also.bin", 0o644, make([]byte, 4096))

	got, err := svc.List(context.Background(), "my-agent", "/app")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !got.Truncated {
		t.Error("Truncated = false; the scan cap must be reported, not silently ignored")
	}
}

func TestListDoesNotTakeTheLock(t *testing.T) {
	svc, _, access := newService(t)

	if _, err := svc.List(context.Background(), "my-agent", "/app"); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if access.locked != 0 {
		t.Errorf("locked = %d, want 0", access.locked)
	}
}
