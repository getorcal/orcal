//go:build docker

package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

func containsFile(items []apigen.FileInfo, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func archiveContains(t *testing.T, rc io.ReadCloser, name string) bool {
	t.Helper()
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		if h.Name == name {
			return true
		}
	}
}

func buildTar(t *testing.T, entries map[string][]byte, modes map[string]int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range entries {
		mode := int64(0o644)
		if m, ok := modes[name]; ok {
			mode = m
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("tar header(%s): %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write(%s): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

func TestFileRoundTripAgainstRealDocker(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "file-roundtrip")
	ctx := context.Background()

	content := []byte("hello from the integration suite\n")
	if err := e.client.WriteFile(ctx, ref, "/tmp/roundtrip.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	info, err := e.client.StatFile(ctx, ref, "/tmp/roundtrip.txt")
	if err != nil {
		t.Fatalf("StatFile() error = %v", err)
	}
	if info.Mode != "0644" {
		t.Errorf("mode = %q, want 0644", info.Mode)
	}
	if info.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", info.Size, len(content))
	}

	rc, err := e.client.ReadFile(ctx, ref, "/tmp/roundtrip.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestArchiveRoundTripPreservesTreeAndModes(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "archive-roundtrip")
	ctx := context.Background()

	envContent := []byte("SECRET=do-not-touch\n")
	if err := e.client.WriteFile(ctx, ref, "/app/.env", bytes.NewReader(envContent)); err != nil {
		t.Fatalf("seed /app/.env error = %v", err)
	}

	tree := map[string][]byte{
		"bin/run.sh":     []byte("#!/bin/sh\necho hi\n"),
		"config/app.yml": []byte("key: value\n"),
		"README.md":      []byte("# hello\n"),
	}
	modes := map[string]int64{
		"bin/run.sh":     0o755,
		"config/app.yml": 0o640,
		"README.md":      0o600,
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, dir := range []string{"bin", "config"} {
		if err := tw.WriteHeader(&tar.Header{Name: dir + "/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatalf("tar dir header(%s): %v", dir, err)
		}
	}
	for _, name := range []string{"bin/run.sh", "config/app.yml", "README.md"} {
		body := tree[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: modes[name], Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("tar file header(%s): %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write(%s): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	if err := e.client.UploadArchive(ctx, ref, "/app", &buf); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}

	envRC, err := e.client.ReadFile(ctx, ref, "/app/.env")
	if err != nil {
		t.Fatalf("ReadFile(/app/.env) error = %v", err)
	}
	gotEnv, err := io.ReadAll(envRC)
	envRC.Close()
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if !bytes.Equal(gotEnv, envContent) {
		t.Errorf(".env content = %q, want %q — the merge guarantee was violated", gotEnv, envContent)
	}

	archiveRC, err := e.client.DownloadArchive(ctx, ref, "/app")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	defer archiveRC.Close()
	tr := tar.NewReader(archiveRC)
	found := map[string]int64{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		found[h.Name] = h.Mode
	}

	for name, wantMode := range modes {
		mode, ok := found["app/"+name]
		if !ok {
			t.Errorf("downloaded archive is missing app/%s", name)
			continue
		}
		if got := fs.FileMode(mode) & fs.ModePerm; got != fs.FileMode(wantMode)&fs.ModePerm {
			t.Errorf("mode of %s = %04o, want %04o", name, got, wantMode)
		}
	}
	if _, ok := found["app/.env"]; !ok {
		t.Error("downloaded archive is missing the pre-existing app/.env")
	}
}

func TestFileOpsWorkOnAStoppedSandbox(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "stopped-file-ops")
	ctx := context.Background()

	before := []byte("before-stop\n")
	if err := e.client.WriteFile(ctx, ref, "/tmp/before-stop.txt", bytes.NewReader(before)); err != nil {
		t.Fatalf("WriteFile() while running error = %v", err)
	}

	if _, err := e.client.StopSandbox(ctx, ref); err != nil {
		t.Fatalf("StopSandbox() error = %v", err)
	}

	info, err := e.client.StatFile(ctx, ref, "/tmp/before-stop.txt")
	if err != nil {
		t.Fatalf("StatFile() while stopped error = %v", err)
	}
	if info.Size != int64(len(before)) {
		t.Errorf("size while stopped = %d, want %d", info.Size, len(before))
	}

	rc, err := e.client.ReadFile(ctx, ref, "/tmp/before-stop.txt")
	if err != nil {
		t.Fatalf("ReadFile() while stopped error = %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if !bytes.Equal(got, before) {
		t.Errorf("content while stopped = %q, want %q", got, before)
	}

	listing, err := e.client.ListFiles(ctx, ref, "/tmp")
	if err != nil {
		t.Fatalf("ListFiles() while stopped error = %v", err)
	}
	if !containsFile(listing.Items, "before-stop.txt") {
		t.Errorf("listing while stopped = %+v, want before-stop.txt", listing.Items)
	}

	archiveRC, err := e.client.DownloadArchive(ctx, ref, "/tmp")
	if err != nil {
		t.Fatalf("DownloadArchive() while stopped error = %v", err)
	}
	if !archiveContains(t, archiveRC, "tmp/before-stop.txt") {
		t.Error("downloaded archive while stopped is missing before-stop.txt")
	}

	after := []byte("after-stop\n")
	if err := e.client.WriteFile(ctx, ref, "/tmp/after-stop.txt", bytes.NewReader(after)); err != nil {
		t.Fatalf("WriteFile() while stopped error = %v", err)
	}

	if _, err := e.client.StartSandbox(ctx, ref); err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}

	out, code := e.runToCompletion(t, ref, "cat", "/tmp/after-stop.txt")
	if code != 0 {
		t.Fatalf("cat exit = %d, want 0", code)
	}
	if strings.TrimSpace(out) != "after-stop" {
		t.Errorf("cat output = %q, want the write made while stopped to have landed", strings.TrimSpace(out))
	}
}

func TestFileOpsWorkOnAnImageWithNoShell(t *testing.T) {
	e := newEnv(t)
	ref := e.sandboxWithImage(t, "no-shell", "traefik/whoami")
	ctx := context.Background()

	info, err := e.client.StatFile(ctx, ref, "/etc/ssl/certs/ca-certificates.crt")
	if err != nil {
		t.Fatalf("StatFile() error = %v", err)
	}
	if info.IsDir {
		t.Error("ca-certificates.crt reported as a directory")
	}
	if info.Size == 0 {
		t.Error("ca-certificates.crt reported as empty")
	}

	rc, err := e.client.ReadFile(ctx, ref, "/etc/ssl/certs/ca-certificates.crt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	certBody, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if int64(len(certBody)) != info.Size {
		t.Errorf("read %d bytes, stat reported %d", len(certBody), info.Size)
	}
	if !bytes.Contains(certBody, []byte("BEGIN CERTIFICATE")) {
		t.Error("ca-certificates.crt does not look like a PEM bundle")
	}

	content := []byte("no shell required\n")
	if err := e.client.WriteFile(ctx, ref, "/data/hello.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	listing, err := e.client.ListFiles(ctx, ref, "/data")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if !containsFile(listing.Items, "hello.txt") {
		t.Errorf("listing = %+v, want hello.txt", listing.Items)
	}

	downloadRC, err := e.client.DownloadArchive(ctx, ref, "/data")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	if !archiveContains(t, downloadRC, "data/hello.txt") {
		t.Error("downloaded archive is missing hello.txt")
	}

	nested := []byte("uploaded without a shell\n")
	archive := buildTar(t, map[string][]byte{"nested.txt": nested}, nil)
	if err := e.client.UploadArchive(ctx, ref, "/data", bytes.NewReader(archive)); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}

	uploadedRC, err := e.client.ReadFile(ctx, ref, "/data/nested.txt")
	if err != nil {
		t.Fatalf("ReadFile(nested.txt) error = %v", err)
	}
	gotNested, err := io.ReadAll(uploadedRC)
	uploadedRC.Close()
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if !bytes.Equal(gotNested, nested) {
		t.Errorf("nested.txt content = %q, want %q", gotNested, nested)
	}

	out, code := e.runToCompletion(t, ref, "/bin/sh")
	if code == 0 {
		t.Errorf("exec of /bin/sh succeeded (output %q) on an image with no shell — the test does not prove what it claims", out)
	}
}

func TestUploadRejectsTraversalAgainstRealDocker(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "upload-traversal")
	ctx := context.Background()

	if err := e.client.WriteFile(ctx, ref, "/upload-dest/.keep", bytes.NewReader(nil)); err != nil {
		t.Fatalf("seed dest dir error = %v", err)
	}

	escapeArchive := buildTar(t, map[string][]byte{"../escape.txt": []byte("pwned")}, nil)
	err := e.client.UploadArchive(ctx, ref, "/upload-dest", bytes.NewReader(escapeArchive))
	var apiErr *orcalclient.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("UploadArchive(traversal) error = %v, want 400", err)
	}

	if _, code := e.runToCompletion(t, ref, "test", "-e", "/escape.txt"); code == 0 {
		t.Error("escape.txt exists at the destination's parent — traversal was not blocked")
	}

	setuidArchive := buildTar(t, map[string][]byte{"setuid-bin": []byte("#!/bin/sh\necho hi\n")}, map[string]int64{"setuid-bin": 0o4755})
	if err := e.client.UploadArchive(ctx, ref, "/upload-dest", bytes.NewReader(setuidArchive)); err != nil {
		t.Fatalf("UploadArchive(setuid) error = %v", err)
	}

	archiveRC, err := e.client.DownloadArchive(ctx, ref, "/upload-dest")
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	defer archiveRC.Close()
	tr := tar.NewReader(archiveRC)
	mode := int64(-1)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		if h.Name == "upload-dest/setuid-bin" {
			mode = h.Mode
		}
	}
	if mode == -1 {
		t.Fatal("uploaded setuid-bin not found in the downloaded archive")
	}
	if mode&0o4000 != 0 {
		t.Errorf("extracted mode = %#o, want the setuid bit (04000) cleared", mode)
	}
}

func TestReadRejectsDeviceFile(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "device-file")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, err := e.client.ReadFile(ctx, ref, "/dev/zero")
	if err == nil {
		rc.Close()
		t.Fatal("ReadFile(/dev/zero) error = nil, want a rejection")
	}
	var apiErr *orcalclient.APIError
	if !asAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("ReadFile(/dev/zero) error = %v, want a 404 (Docker has no archive-layer entry for a runtime device node)", err)
	}
}

func TestFileOpsReportVanishedContainerNotMissingPath(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "vanished-container")
	ctx := context.Background()

	if err := e.client.WriteFile(ctx, ref, "/app/seed.txt", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("seed WriteFile() error = %v", err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()
	containerID := containerIDFor(t, cli, ref)
	if err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		t.Fatalf("docker rm -f: %v", err)
	}

	assertVanishedContainerReported := func(t *testing.T, err error) {
		t.Helper()
		var apiErr *orcalclient.APIError
		if !asAPIError(err, &apiErr) {
			t.Fatalf("error = %v, want an *orcalclient.APIError", err)
		}
		if apiErr.Code != "invalid_state" || apiErr.StatusCode != http.StatusConflict {
			t.Errorf("code = %q status = %d, want invalid_state/409 like any other runtime.ErrNotFound; got path_not_found if the container/path distinction was lost", apiErr.Code, apiErr.StatusCode)
		}
	}

	_, statErr := e.client.StatFile(ctx, ref, "/app/seed.txt")
	assertVanishedContainerReported(t, statErr)

	_, readErr := e.client.ReadFile(ctx, ref, "/app/seed.txt")
	assertVanishedContainerReported(t, readErr)

	_, listErr := e.client.ListFiles(ctx, ref, "/app")
	assertVanishedContainerReported(t, listErr)
}

func TestConcurrentUploadAndSnapshotBothSucceedWithAConsistentForkedTree(t *testing.T) {
	e := newEnv(t)
	ref := e.sandbox(t, "concurrent-upload")
	ctx := context.Background()

	if _, code := e.runToCompletion(t, ref, "mkdir", "-p", "/upload-target"); code != 0 {
		t.Fatalf("mkdir setup exit = %d, want 0", code)
	}

	const fileCount = 60
	const fileSize = 6000
	entries := make(map[string][]byte, fileCount)
	for i := range fileCount {
		entries[fmt.Sprintf("f%03d.bin", i)] = bytes.Repeat([]byte{byte(i % 256)}, fileSize)
	}
	archive := buildTar(t, entries, nil)

	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- e.client.UploadArchive(ctx, ref, "/upload-target", bytes.NewReader(archive))
	}()

	snapID := e.snapshot(t, ref, "")

	if err := <-uploadErr; err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}

	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "concurrent-upload-fork", Snapshot: snapID})
	if err != nil {
		t.Fatalf("fork error = %v", err)
	}
	e.sandboxes = append(e.sandboxes, forked.Id)

	present := 0
	for name, want := range entries {
		rc, err := e.client.ReadFile(ctx, forked.Id, "/upload-target/"+name)
		if err != nil {
			var apiErr *orcalclient.APIError
			if asAPIError(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				continue
			}
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read body(%s): %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is %d bytes, want 0 (absent) or %d bytes intact — a partial file means the snapshot raced the upload", name, len(got), fileSize)
		} else {
			present++
		}
	}
	t.Logf("fork tree contains %d/%d uploaded files, each fully present or fully absent", present, fileCount)
}
