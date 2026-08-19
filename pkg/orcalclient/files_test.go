package orcalclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestClientWriteThenReadFileRoundTrips(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})

	body := []byte("hello world")
	if err := c.WriteFile(ctx, "my-agent", "/app/a.txt", bytes.NewReader(body)); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rc, err := c.ReadFile(ctx, "my-agent", "/app/a.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestClientReadFileBodyIsReadableAfterReturn(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	c.WriteFile(ctx, "my-agent", "/app/a.txt", bytes.NewReader([]byte("payload")))

	rc, err := c.ReadFile(ctx, "my-agent", "/app/a.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("body not readable after ReadFile() returned: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("body = %q, want %q", got, "payload")
	}
}

func TestClientStatFile(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	c.WriteFile(ctx, "my-agent", "/app/a.txt", bytes.NewReader([]byte("hi")))

	info, err := c.StatFile(ctx, "my-agent", "/app/a.txt")
	if err != nil {
		t.Fatalf("StatFile() error = %v", err)
	}
	if info.IsDir {
		t.Error("is_dir = true, want false")
	}
	if info.Size != 2 {
		t.Errorf("size = %d, want 2", info.Size)
	}
}

func TestClientListFiles(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	c.WriteFile(ctx, "my-agent", "/app/a.txt", bytes.NewReader([]byte("a")))
	c.WriteFile(ctx, "my-agent", "/app/b.txt", bytes.NewReader([]byte("b")))

	list, err := c.ListFiles(ctx, "my-agent", "/app")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	names := map[string]bool{}
	for _, item := range list.Items {
		names[item.Name] = true
	}
	if !names["a.txt"] || !names["b.txt"] {
		t.Errorf("items = %v, want a.txt and b.txt", list.Items)
	}
}

func TestClientFileNotFoundSurfacesAsAPIError(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})

	_, err := c.StatFile(ctx, "my-agent", "/nope")

	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *orcalclient.APIError", err)
	}
	if apiErr.Code != "path_not_found" {
		t.Errorf("Code = %q, want path_not_found", apiErr.Code)
	}
}

func TestClientDownloadThenUploadArchiveRoundTrips(t *testing.T) {
	c, f, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	runtimeID := f.IDForSandbox(mustSandboxID(t, c, ctx, "my-agent"))
	f.Seed(runtimeID, "/app/a.txt", 0o644, []byte("archived"))

	rc, err := c.DownloadArchive(ctx, "my-agent", "/app")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	defer rc.Close()
	tarBody, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if len(tarBody) == 0 {
		t.Fatal("archive body is empty")
	}

	f.SeedDir(runtimeID, "/restore", 0o755)
	if err := c.UploadArchive(ctx, "my-agent", "/restore", bytes.NewReader(tarBody)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}

	info, err := c.StatFile(ctx, "my-agent", "/restore/app/a.txt")
	if err != nil {
		t.Fatalf("StatFile() after upload error = %v", err)
	}
	if info.Size != int64(len("archived")) {
		t.Errorf("size = %d, want %d", info.Size, len("archived"))
	}
}

func TestClientDownloadArchiveBodyIsReadableAfterReturn(t *testing.T) {
	c, f, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine:3.20"})
	runtimeID := f.IDForSandbox(mustSandboxID(t, c, ctx, "my-agent"))
	f.Seed(runtimeID, "/app/a.txt", 0o644, []byte("archived"))

	rc, err := c.DownloadArchive(ctx, "my-agent", "/app")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("body not readable after DownloadArchive() returned: %v", err)
	}
	if len(got) == 0 {
		t.Error("body is empty, want tar bytes")
	}
}

func TestClientEscapesRefsContainingSlashesAndSpaces(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()

	weird := "a b/c"
	if _, err := c.StatFile(ctx, weird, "/app/a.txt"); err == nil {
		t.Fatal("StatFile() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an APIError rather than a corrupted request", err)
		}
		if apiErr.Code != "sandbox_not_found" {
			t.Errorf("Code = %q, want sandbox_not_found (proves the ref reached the server as one segment)", apiErr.Code)
		}
	}

	if err := c.WriteFile(ctx, weird, "/app/a.txt", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("WriteFile() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "sandbox_not_found" {
			t.Errorf("WriteFile() error = %v, want sandbox_not_found", err)
		}
	}

	if _, err := c.ReadFile(ctx, weird, "/app/a.txt"); err == nil {
		t.Fatal("ReadFile() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "sandbox_not_found" {
			t.Errorf("ReadFile() error = %v, want sandbox_not_found", err)
		}
	}

	if _, err := c.ListFiles(ctx, weird, "/app"); err == nil {
		t.Fatal("ListFiles() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "sandbox_not_found" {
			t.Errorf("ListFiles() error = %v, want sandbox_not_found", err)
		}
	}

	if _, err := c.DownloadArchive(ctx, weird, "/app"); err == nil {
		t.Fatal("DownloadArchive() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "sandbox_not_found" {
			t.Errorf("DownloadArchive() error = %v, want sandbox_not_found", err)
		}
	}

	if err := c.UploadArchive(ctx, weird, "/app", bytes.NewReader(nil)); err == nil {
		t.Fatal("UploadArchive() error = nil, want an APIError for a nonexistent sandbox")
	} else {
		var apiErr *orcalclient.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "sandbox_not_found" {
			t.Errorf("UploadArchive() error = %v, want sandbox_not_found", err)
		}
	}
}

func mustSandboxID(t *testing.T, c *orcalclient.Client, ctx context.Context, ref string) string {
	t.Helper()
	got, err := c.GetSandbox(ctx, ref)
	if err != nil {
		t.Fatalf("GetSandbox(%q) error = %v", ref, err)
	}
	return got.Id
}
