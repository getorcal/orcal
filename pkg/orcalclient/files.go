package orcalclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/getorcal/orcal/internal/apigen"
)

func filePathQuery(p string) url.Values {
	q := url.Values{}
	q.Set("path", p)
	return q
}

func (c *Client) newStreamRequest(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("orcal: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)
	return req, nil
}

func (c *Client) ReadFile(ctx context.Context, ref, path string) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref)+"/files?"+filePathQuery(path).Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) WriteFile(ctx context.Context, ref, path string, body io.Reader) error {
	req, err := c.newStreamRequest(ctx, http.MethodPut, "/v1/sandboxes/"+url.PathEscape(ref)+"/files?"+filePathQuery(path).Encode(), body, "application/octet-stream")
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (c *Client) StatFile(ctx context.Context, ref, path string) (*apigen.FileInfo, error) {
	return do[apigen.FileInfo](c, ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref)+"/files/stat?"+filePathQuery(path).Encode(), nil)
}

func (c *Client) ListFiles(ctx context.Context, ref, path string) (*apigen.FileList, error) {
	return do[apigen.FileList](c, ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref)+"/files/list?"+filePathQuery(path).Encode(), nil)
}

func (c *Client) DownloadArchive(ctx context.Context, ref, path string) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref)+"/archive?"+filePathQuery(path).Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) UploadArchive(ctx context.Context, ref, path string, tarStream io.Reader) error {
	req, err := c.newStreamRequest(ctx, http.MethodPut, "/v1/sandboxes/"+url.PathEscape(ref)+"/archive?"+filePathQuery(path).Encode(), tarStream, "application/x-tar")
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
