package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/audit"
)

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := audit.Filter{
		Actor:        query.Get("actor"),
		Action:       audit.Action(query.Get("action")),
		ResourceType: query.Get("resource_type"),
		ResourceID:   query.Get("resource_id"),
		Limit:        pageLimit(r),
		Cursor:       query.Get("cursor"),
	}

	var err error
	if filter.Since, err = parseTimeParam(query.Get("since")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if filter.Until, err = parseTimeParam(query.Get("until")); err != nil {
		s.writeError(w, r, err)
		return
	}

	items, err := s.audit.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	out := apigen.EventList{Items: make([]apigen.Event, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, toAPIEvent(item))
	}
	if len(items) == filter.Limit && len(items) > 0 {
		out.NextCursor = ptr(items[len(items)-1].ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func parseTimeParam(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not an RFC 3339 timestamp", ErrInvalidRequest, raw)
	}
	return parsed.UTC(), nil
}
