package orcalclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/getorcal/orcal/internal/apigen"
)

type CreateTokenParams struct {
	Name             string   `json:"name"`
	Scopes           []string `json:"scopes"`
	ExpiresInSeconds int64    `json:"expires_in_seconds,omitempty"`
}

func (c *Client) CreateToken(ctx context.Context, params CreateTokenParams) (*apigen.CreatedToken, error) {
	return do[apigen.CreatedToken](c, ctx, http.MethodPost, "/v1/tokens", params)
}

func (c *Client) ListTokens(ctx context.Context) (*apigen.TokenList, error) {
	return do[apigen.TokenList](c, ctx, http.MethodGet, "/v1/tokens", nil)
}

func (c *Client) RevokeToken(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/tokens/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
