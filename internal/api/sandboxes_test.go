package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

type harness struct {
	server *httptest.Server
	fake   *fake.Fake
	execs  *exec.Service
	token  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	sandboxes := sandbox.NewService(st.Sandboxes(), f, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"})
	execs, err := exec.NewService(st.Execs(), sandboxes, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("exec.NewService() error = %v", err)
	}
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, f)
	sandboxes.SetSnapshots(snapshots)
	fileSvc := files.NewService(sandboxes, f, files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   1000,
		ListMaxScanBytes: 1 << 20,
	})

	tokens := auth.NewService(auth.NewMemoryRepo())
	_, token, err := tokens.Create(context.Background(), auth.CreateOptions{Name: "test", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("mint test token: %v", err)
	}

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		Snapshots: snapshots,
		Files:     fileSvc,
		Tokens:    tokens,
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	return &harness{server: srv, fake: f, execs: execs, token: token}
}

func (h *harness) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (h *harness) doCapturing(t *testing.T, method, path string, body any) (*http.Response, *http.Request, []byte) {
	t.Helper()
	resp := h.do(t, method, path, body)
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

func readCloser(b []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(b))
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func createSandbox(t *testing.T, h *harness, name string) apigen.Sandbox {
	t.Helper()
	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": name, "image": "alpine"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	return decode[apigen.Sandbox](t, resp)
}

func TestCreateSandboxReturns201AndRunningState(t *testing.T) {
	h := newHarness(t)

	got := createSandbox(t, h, "my-agent")

	if got.State != "running" {
		t.Errorf("state = %q, want running", got.State)
	}
	if got.Name == nil || *got.Name != "my-agent" {
		t.Errorf("name = %v, want my-agent", got.Name)
	}
	if got.Resources.CpuMillis != 1000 {
		t.Errorf("cpu_millis = %d, want the server default 1000", got.Resources.CpuMillis)
	}
	if got.Id == "" {
		t.Error("id is empty")
	}
}

func TestCreateSandboxWithoutImageReturns400InvalidRequest(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": "no-image"})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestCreateSandboxWithNegativeResourceLimitsReturns400InvalidRequest(t *testing.T) {
	cases := []struct {
		name  string
		field string
	}{
		{"negative pids limit", "pids_limit"},
		{"negative cpu millis", "cpu_millis"},
		{"negative memory bytes", "memory_bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)

			resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"image": "alpine", c.field: -1})

			if resp.StatusCode != http.StatusBadRequest {
				resp.Body.Close()
				t.Fatalf("status = %d, want 400 for %s = -1", resp.StatusCode, c.field)
			}
			body := decode[apigen.Error](t, resp)
			if body.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", body.Error.Code)
			}
		})
	}
}

func TestCreateSandboxWithDuplicateNameReturns409NameTaken(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": "my-agent", "image": "alpine"})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "name_taken" {
		t.Errorf("code = %q, want name_taken", body.Error.Code)
	}
}

func TestGetSandboxResolvesByNameAndByID(t *testing.T) {
	h := newHarness(t)
	created := createSandbox(t, h, "my-agent")

	byName := decode[apigen.Sandbox](t, h.do(t, http.MethodGet, "/v1/sandboxes/my-agent", nil))
	byID := decode[apigen.Sandbox](t, h.do(t, http.MethodGet, "/v1/sandboxes/"+created.Id, nil))

	if byName.Id != created.Id || byID.Id != created.Id {
		t.Errorf("byName.Id = %s, byID.Id = %s, want %s", byName.Id, byID.Id, created.Id)
	}
}

func TestGetUnknownSandboxReturns404SandboxNotFound(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, http.MethodGet, "/v1/sandboxes/ghost", nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "sandbox_not_found" {
		t.Errorf("code = %q, want sandbox_not_found", body.Error.Code)
	}
}

func TestStopThenStartTransitionsState(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	stopped := decode[apigen.Sandbox](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/stop", nil))
	if stopped.State != "stopped" {
		t.Errorf("state after stop = %q, want stopped", stopped.State)
	}

	started := decode[apigen.Sandbox](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/start", nil))
	if started.State != "running" {
		t.Errorf("state after start = %q, want running", started.State)
	}
}

func TestStartRunningSandboxReturns409InvalidState(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/start", nil)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "invalid_state" {
		t.Errorf("code = %q, want invalid_state", body.Error.Code)
	}
}

func TestDestroySandboxReturnsDestroyedAndRemovesItFromList(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	destroyed := decode[apigen.Sandbox](t, h.do(t, http.MethodDelete, "/v1/sandboxes/my-agent", nil))
	if destroyed.State != "destroyed" {
		t.Errorf("state = %q, want destroyed", destroyed.State)
	}

	list := decode[apigen.SandboxList](t, h.do(t, http.MethodGet, "/v1/sandboxes", nil))
	if len(list.Items) != 0 {
		t.Errorf("list has %d items after destroy, want 0", len(list.Items))
	}
}

func TestListPaginatesAndReturnsNextCursor(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "agent-a")
	createSandbox(t, h, "agent-b")
	createSandbox(t, h, "agent-c")

	page := decode[apigen.SandboxList](t, h.do(t, http.MethodGet, "/v1/sandboxes?limit=2", nil))
	if len(page.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(page.Items))
	}
	if page.NextCursor == nil || *page.NextCursor != page.Items[1].Id {
		t.Fatalf("next_cursor = %v, want %s", page.NextCursor, page.Items[1].Id)
	}

	rest := decode[apigen.SandboxList](t, h.do(t, http.MethodGet, "/v1/sandboxes?limit=2&cursor="+*page.NextCursor, nil))
	if len(rest.Items) != 1 {
		t.Errorf("len(items) on second page = %d, want 1", len(rest.Items))
	}
	if rest.NextCursor != nil {
		t.Errorf("next_cursor on last page = %v, want nil", rest.NextCursor)
	}
}

func TestListFiltersByLabel(t *testing.T) {
	h := newHarness(t)
	h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{
		"name": "core-agent", "image": "alpine", "labels": map[string]string{"team": "core"},
	})
	h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{
		"name": "growth-agent", "image": "alpine", "labels": map[string]string{"team": "growth"},
	})

	list := decode[apigen.SandboxList](t, h.do(t, http.MethodGet, "/v1/sandboxes?label=team%3Dcore", nil))

	if len(list.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(list.Items))
	}
	if list.Items[0].Name == nil || *list.Items[0].Name != "core-agent" {
		t.Errorf("item = %v, want core-agent", list.Items[0].Name)
	}
}

func TestRequestsWithoutTokenAreRejected(t *testing.T) {
	h := newHarness(t)

	resp, err := h.server.Client().Get(h.server.URL + "/v1/sandboxes")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHealthAndVersionAreUnauthenticated(t *testing.T) {
	h := newHarness(t)

	health, err := h.server.Client().Get(h.server.URL + "/v1/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", health.StatusCode)
	}

	version, err := h.server.Client().Get(h.server.URL + "/v1/version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version.StatusCode != http.StatusOK {
		t.Errorf("version status = %d, want 200", version.StatusCode)
	}
	body := decode[apigen.Version](t, version)
	if body.Version != "test" {
		t.Errorf("version = %q, want test", body.Version)
	}
	if len(body.ApiVersions) == 0 || body.ApiVersions[0] != "v1" {
		t.Errorf("api_versions = %v, want [v1]", body.ApiVersions)
	}
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, http.MethodGet, "/v1/sandboxes", nil)
	defer resp.Body.Close()

	if resp.Header.Get(api.RequestIDHeader) == "" {
		t.Errorf("%s header is empty, want a generated request id", api.RequestIDHeader)
	}
}
