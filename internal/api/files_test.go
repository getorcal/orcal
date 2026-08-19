package api_test

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/apigen"
)

func (h *harness) doRaw(t *testing.T, method, path, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (h *harness) doRawCapturing(t *testing.T, method, path, contentType string, body []byte) (*http.Response, *http.Request, []byte) {
	t.Helper()
	resp := h.doRaw(t, method, path, contentType, body)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	validated, err := http.NewRequest(method, "http://api"+path, nil)
	if err != nil {
		t.Fatalf("build validation request: %v", err)
	}
	return resp, validated, raw
}

func buildTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func TestFileWriteThenReadRoundTrips(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")

	body := []byte("hello world")
	putResp := h.doRaw(t, http.MethodPut, "/v1/sandboxes/"+created.Id+"/files?path=/app/a.txt", "application/octet-stream", body)
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", putResp.StatusCode)
	}

	getResp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files?path=/app/a.txt", "", nil)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	got, err := io.ReadAll(getResp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
	if cl := getResp.Header.Get("Content-Length"); cl != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(body))
	}
}

func TestFileGetOnDirectoryReturns400InvalidRequest(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.SeedDir(runtimeID, "/app", 0o755)

	resp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files?path=/app", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != apigen.InvalidRequest {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestFilePathNotFoundDiffersFromSandboxNotFound(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.SeedDir(runtimeID, "/app", 0o755)

	missingPath := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files?path=/nope", "", nil)
	if missingPath.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", missingPath.StatusCode)
	}
	pathBody := decode[apigen.Error](t, missingPath)
	if pathBody.Error.Code != apigen.PathNotFound {
		t.Errorf("code = %q, want path_not_found", pathBody.Error.Code)
	}

	missingSandbox := h.doRaw(t, http.MethodGet, "/v1/sandboxes/ghost/files?path=/app", "", nil)
	if missingSandbox.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", missingSandbox.StatusCode)
	}
	sandboxBody := decode[apigen.Error](t, missingSandbox)
	if sandboxBody.Error.Code != apigen.SandboxNotFound {
		t.Errorf("code = %q, want sandbox_not_found", sandboxBody.Error.Code)
	}

	if pathBody.Error.Code == sandboxBody.Error.Code {
		t.Errorf("path_not_found and sandbox_not_found must differ, both were %q", pathBody.Error.Code)
	}
}

func TestFileGetWithRelativePathReturns400InvalidRequest(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")

	resp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files?path=relative", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != apigen.InvalidRequest {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestFileStatReturnsModeAndIsDir(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))

	resp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files/stat?path=/app/a.txt", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decode[apigen.FileInfo](t, resp)
	if got.IsDir {
		t.Error("is_dir = true, want false")
	}
	if got.Mode != "0644" {
		t.Errorf("mode = %q, want 0644", got.Mode)
	}
}

func TestFileListReturnsSeededEntries(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))
	h.fake.Seed(runtimeID, "/app/b.txt", 0o644, []byte("world"))

	resp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/files/list?path=/app", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decode[apigen.FileList](t, resp)
	if got.Truncated {
		t.Error("truncated = true, want false")
	}
	names := map[string]bool{}
	for _, item := range got.Items {
		names[item.Name] = true
	}
	if !names["a.txt"] || !names["b.txt"] {
		t.Errorf("items = %v, want a.txt and b.txt", got.Items)
	}
}

func TestFileUploadArchiveThenMissingDestinationReturns404PathNotFound(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.SeedDir(runtimeID, "/app", 0o755)
	tarBody := buildTar(t, map[string][]byte{"b.txt": []byte("uploaded")})

	okResp := h.doRaw(t, http.MethodPut, "/v1/sandboxes/"+created.Id+"/archive?path=/app", "application/x-tar", tarBody)
	if okResp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", okResp.StatusCode)
	}

	missingResp := h.doRaw(t, http.MethodPut, "/v1/sandboxes/"+created.Id+"/archive?path=/missing", "application/x-tar", tarBody)
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT missing status = %d, want 404", missingResp.StatusCode)
	}
	body := decode[apigen.Error](t, missingResp)
	if body.Error.Code != apigen.PathNotFound {
		t.Errorf("code = %q, want path_not_found", body.Error.Code)
	}
}

func TestFileDownloadArchiveReturnsReadableTar(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")
	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))

	resp := h.doRaw(t, http.MethodGet, "/v1/sandboxes/"+created.Id+"/archive?path=/app", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-tar" {
		t.Errorf("Content-Type = %q, want application/x-tar", ct)
	}

	tr := tar.NewReader(resp.Body)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if strings.Contains(hdr.Name, "a.txt") {
			found = true
		}
	}
	if !found {
		t.Error("tar did not contain a.txt")
	}
}

func TestFileWriteOverLimitReturns413ResourceExhausted(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")

	body := bytes.Repeat([]byte("a"), (1<<20)+10)
	resp := h.doRaw(t, http.MethodPut, "/v1/sandboxes/"+created.Id+"/files?path=/app/big.txt", "application/octet-stream", body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	respBody := decode[apigen.Error](t, resp)
	if respBody.Error.Code != apigen.ResourceExhausted {
		t.Errorf("code = %q, want resource_exhausted", respBody.Error.Code)
	}
}

func TestFileRoutesRequireAuthentication(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/sandboxes/" + created.Id + "/files?path=/app"},
		{http.MethodPut, "/v1/sandboxes/" + created.Id + "/files?path=/app/a.txt"},
		{http.MethodGet, "/v1/sandboxes/" + created.Id + "/files/stat?path=/app"},
		{http.MethodGet, "/v1/sandboxes/" + created.Id + "/files/list?path=/app"},
		{http.MethodGet, "/v1/sandboxes/" + created.Id + "/archive?path=/app"},
		{http.MethodPut, "/v1/sandboxes/" + created.Id + "/archive?path=/app"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req, err := http.NewRequest(route.method, h.server.URL+route.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := h.server.Client().Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}
