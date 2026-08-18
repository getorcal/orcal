package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/sandbox"
)

const defaultPageLimit = 50

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	var req apigen.CreateSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: malformed JSON body", sandbox.ErrInvalidName))
		return
	}

	var opts sandbox.CreateOptions
	if req.Image != nil {
		opts.Image = *req.Image
	}
	if req.Name != nil {
		opts.Name = *req.Name
	}
	if req.Env != nil {
		opts.Env = *req.Env
	}
	if req.Labels != nil {
		opts.Labels = *req.Labels
	}
	if req.CpuMillis != nil {
		opts.Resources.CPUMillis = *req.CpuMillis
	}
	if req.MemoryBytes != nil {
		opts.Resources.MemoryBytes = *req.MemoryBytes
	}
	if req.PidsLimit != nil {
		opts.Resources.PidsLimit = *req.PidsLimit
	}

	created, err := s.sandboxes.Create(r.Context(), opts)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPISandbox(created))
}

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	filter := sandbox.Filter{Limit: pageLimit(r), Cursor: r.URL.Query().Get("cursor")}
	if state := r.URL.Query().Get("state"); state != "" {
		filter.States = []sandbox.State{sandbox.State(state)}
	}
	if labels := r.URL.Query()["label"]; len(labels) > 0 {
		filter.Labels = map[string]string{}
		for _, pair := range labels {
			key, value, found := strings.Cut(pair, "=")
			if !found {
				s.writeError(w, r, fmt.Errorf("%w: label %q must be key=value", sandbox.ErrInvalidName, pair))
				return
			}
			filter.Labels[key] = value
		}
	}

	items, err := s.sandboxes.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	out := apigen.SandboxList{Items: make([]apigen.Sandbox, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, toAPISandbox(item))
	}
	if len(items) == filter.Limit && len(items) > 0 {
		out.NextCursor = ptr(items[len(items)-1].ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	got, err := s.sandboxes.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISandbox(got))
}

func (s *Server) handleStartSandbox(w http.ResponseWriter, r *http.Request) {
	s.respondWithSandbox(w, r, s.sandboxes.Start)
}

func (s *Server) handleStopSandbox(w http.ResponseWriter, r *http.Request) {
	s.respondWithSandbox(w, r, s.sandboxes.Stop)
}

func (s *Server) handleDestroySandbox(w http.ResponseWriter, r *http.Request) {
	s.respondWithSandbox(w, r, s.sandboxes.Destroy)
}

func (s *Server) respondWithSandbox(w http.ResponseWriter, r *http.Request, act func(ctx context.Context, ref string) (*sandbox.Sandbox, error)) {
	result, err := act(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISandbox(result))
}

func pageLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultPageLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 200 {
		return defaultPageLimit
	}
	return n
}
