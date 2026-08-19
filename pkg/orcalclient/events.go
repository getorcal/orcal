package orcalclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
)

type ListEventsParams struct {
	Actor        string
	Action       string
	ResourceType string
	ResourceID   string
	Since        time.Time
	Until        time.Time
	Limit        int
	Cursor       string
}

func (p ListEventsParams) query() url.Values {
	q := url.Values{}
	if p.Actor != "" {
		q.Set("actor", p.Actor)
	}
	if p.Action != "" {
		q.Set("action", p.Action)
	}
	if p.ResourceType != "" {
		q.Set("resource_type", p.ResourceType)
	}
	if p.ResourceID != "" {
		q.Set("resource_id", p.ResourceID)
	}
	if !p.Since.IsZero() {
		q.Set("since", p.Since.UTC().Format(time.RFC3339))
	}
	if !p.Until.IsZero() {
		q.Set("until", p.Until.UTC().Format(time.RFC3339))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	return q
}

func (c *Client) ListEvents(ctx context.Context, params ListEventsParams) (*apigen.EventList, error) {
	return do[apigen.EventList](c, ctx, http.MethodGet, "/v1/events?"+params.query().Encode(), nil)
}
