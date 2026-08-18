package orcalclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/getorcal/orcal/internal/apigen"
)

type CreateSandboxParams struct {
	Name        string            `json:"name,omitempty"`
	Image       string            `json:"image"`
	CPUMillis   int               `json:"cpu_millis,omitempty"`
	MemoryBytes int64             `json:"memory_bytes,omitempty"`
	PidsLimit   int               `json:"pids_limit,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

func (c *Client) CreateSandbox(ctx context.Context, params CreateSandboxParams) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodPost, "/v1/sandboxes", params)
}

func (c *Client) ListSandboxes(ctx context.Context, params ListParams) (*apigen.SandboxList, error) {
	return do[apigen.SandboxList](c, ctx, http.MethodGet, "/v1/sandboxes?"+params.query().Encode(), nil)
}

func (c *Client) GetSandbox(ctx context.Context, ref string) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref), nil)
}

func (c *Client) StartSandbox(ctx context.Context, ref string) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(ref)+"/start", nil)
}

func (c *Client) StopSandbox(ctx context.Context, ref string) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(ref)+"/stop", nil)
}

func (c *Client) DestroySandbox(ctx context.Context, ref string) (*apigen.Sandbox, error) {
	return do[apigen.Sandbox](c, ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(ref), nil)
}

func (c *Client) Version(ctx context.Context) (*apigen.Version, error) {
	return do[apigen.Version](c, ctx, http.MethodGet, "/v1/version", nil)
}
