package orcalclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/getorcal/orcal/internal/apigen"
)

type CreateSnapshotParams struct {
	Name string `json:"name,omitempty"`
}

type restoreParams struct {
	Snapshot string `json:"snapshot"`
}

func (c *Client) CreateSnapshot(ctx context.Context, sandboxRef string, params CreateSnapshotParams) (*apigen.Snapshot, error) {
	return do[apigen.Snapshot](c, ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxRef)+"/snapshots", params)
}

func (c *Client) ListSandboxSnapshots(ctx context.Context, sandboxRef string, params ListParams) (*apigen.SnapshotList, error) {
	return do[apigen.SnapshotList](c, ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(sandboxRef)+"/snapshots?"+params.query().Encode(), nil)
}

func (c *Client) ListSnapshots(ctx context.Context, params ListParams) (*apigen.SnapshotList, error) {
	return do[apigen.SnapshotList](c, ctx, http.MethodGet, "/v1/snapshots?"+params.query().Encode(), nil)
}

func (c *Client) GetSnapshot(ctx context.Context, ref string) (*apigen.Snapshot, error) {
	return do[apigen.Snapshot](c, ctx, http.MethodGet, "/v1/snapshots/"+url.PathEscape(ref), nil)
}

func (c *Client) DeleteSnapshot(ctx context.Context, ref string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/snapshots/"+url.PathEscape(ref), nil)
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (c *Client) RestoreSandbox(ctx context.Context, sandboxRef, snapshotRef string) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxRef)+"/restore", restoreParams{Snapshot: snapshotRef})
}
