package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/getorcal/orcal/internal/apigen"
)

type onlyReader struct{ r *bytes.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func TestCreateSnapshotReturns201(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "working-v1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decode[apigen.Snapshot](t, resp)
	if got.Id == "" {
		t.Error("id is empty")
	}
	if got.Name == nil || *got.Name != "working-v1" {
		t.Errorf("name = %v, want working-v1", got.Name)
	}
	if got.SizeBytes <= 0 {
		t.Errorf("size_bytes = %d, want positive", got.SizeBytes)
	}
	if got.ParentId != nil {
		t.Errorf("parent_id = %v, want nil", got.ParentId)
	}
}

func TestCreateSnapshotWithInvalidNameReturns400(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "Bad_Name"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if body := decode[apigen.Error](t, resp); body.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestCreateSnapshotOnUnknownSandboxReturns404(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, http.MethodPost, "/v1/sandboxes/ghost/snapshots", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetUnknownSnapshotReturns404SnapshotNotFound(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, http.MethodGet, "/v1/snapshots/ghost", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if body := decode[apigen.Error](t, resp); body.Error.Code != "snapshot_not_found" {
		t.Errorf("code = %q, want snapshot_not_found", body.Error.Code)
	}
}

func TestListSnapshotsForASandbox(t *testing.T) {
	h := newHarness(t)
	agent := createSandbox(t, h, "my-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"})
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v2"})

	createSandbox(t, h, "other-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/other-agent/snapshots", map[string]any{"name": "other-v1"})

	list := decode[apigen.SnapshotList](t, h.do(t, http.MethodGet, "/v1/sandboxes/my-agent/snapshots", nil))
	if len(list.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(list.Items))
	}
	for _, item := range list.Items {
		if item.SandboxId != agent.Id {
			t.Errorf("item.sandbox_id = %q, want %q", item.SandboxId, agent.Id)
		}
	}
}

func TestListSnapshotsFiltersByQueryParam(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"})

	createSandbox(t, h, "other-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/other-agent/snapshots", map[string]any{"name": "other-v1"})

	list := decode[apigen.SnapshotList](t, h.do(t, http.MethodGet, "/v1/snapshots?sandbox=my-agent", nil))
	if len(list.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(list.Items))
	}
	if list.Items[0].Name == nil || *list.Items[0].Name != "v1" {
		t.Errorf("name = %v, want v1", list.Items[0].Name)
	}
}

func TestCreateSandboxFromSnapshotSetsLineage(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	snap := decode[apigen.Snapshot](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"}))

	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": "experiment-a", "snapshot": "v1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	forked := decode[apigen.Sandbox](t, resp)
	if forked.ParentSnapshotId == nil || *forked.ParentSnapshotId != snap.Id {
		t.Errorf("parent_snapshot_id = %v, want %q", forked.ParentSnapshotId, snap.Id)
	}
}

func TestCreateSandboxWithBothImageAndSnapshotReturns400(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"image": "alpine", "snapshot": "v1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if body := decode[apigen.Error](t, resp); body.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestCreateSandboxWithNeitherImageNorSnapshotReturns400(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": "nothing"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRestoreReturnsTheSandbox(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	snap := decode[apigen.Snapshot](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"}))

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/restore", map[string]any{"snapshot": "v1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decode[apigen.Sandbox](t, resp)
	if got.State != "running" {
		t.Errorf("state = %q, want running", got.State)
	}
	if got.ParentSnapshotId == nil || *got.ParentSnapshotId != snap.Id {
		t.Errorf("parent_snapshot_id = %v, want %q", got.ParentSnapshotId, snap.Id)
	}
}

func TestRestoreWithoutASnapshotFieldReturns400(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/restore", map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDeleteSnapshotReturns204(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	snap := decode[apigen.Snapshot](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"}))

	resp := h.do(t, http.MethodDelete, "/v1/snapshots/"+snap.Id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if after := h.do(t, http.MethodGet, "/v1/snapshots/"+snap.Id, nil); after.StatusCode != http.StatusNotFound {
		t.Errorf("status after delete = %d, want 404", after.StatusCode)
	}
}

func TestDeleteSnapshotWithDescendantsReturns409(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	parent := decode[apigen.Snapshot](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"}))
	h.do(t, http.MethodPost, "/v1/sandboxes", map[string]any{"name": "child", "snapshot": "v1"})
	h.do(t, http.MethodPost, "/v1/sandboxes/child/snapshots", map[string]any{"name": "v2"})

	resp := h.do(t, http.MethodDelete, "/v1/snapshots/"+parent.Id, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if body := decode[apigen.Error](t, resp); body.Error.Code != "invalid_state" {
		t.Errorf("code = %q, want invalid_state", body.Error.Code)
	}
}

func TestCreateSnapshotWithChunkedBodyIsNotSilentlyDropped(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	encoded, err := json.Marshal(map[string]any{"name": "chunky"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/v1/sandboxes/my-agent/snapshots", onlyReader{bytes.NewReader(encoded)})
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	if req.ContentLength != 0 {
		t.Fatalf("ContentLength = %d, want 0 (unknown) so the transport sends this chunked", req.ContentLength)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decode[apigen.Snapshot](t, resp)
	if got.Name == nil || *got.Name != "chunky" {
		t.Errorf("name = %v, want chunky", got.Name)
	}
}

func TestCreateSnapshotWithNoBodyReturns201(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decode[apigen.Snapshot](t, resp)
	if got.Name != nil {
		t.Errorf("name = %v, want nil", got.Name)
	}
}
