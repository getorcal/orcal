package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/snapshot"
)

func (s *Server) registerSnapshotRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/sandboxes/{ref}/snapshots", s.handleCreateSnapshot)
	mux.HandleFunc("GET /v1/sandboxes/{ref}/snapshots", s.handleListSandboxSnapshots)
	mux.HandleFunc("POST /v1/sandboxes/{ref}/restore", s.handleRestoreSandbox)
	mux.HandleFunc("GET /v1/snapshots", s.handleListSnapshots)
	mux.HandleFunc("GET /v1/snapshots/{ref}", s.handleGetSnapshot)
	mux.HandleFunc("DELETE /v1/snapshots/{ref}", s.handleDeleteSnapshot)
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req apigen.CreateSnapshotRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, r, fmt.Errorf("%w: malformed JSON body", ErrInvalidRequest))
			return
		}
	}

	opts := snapshot.CreateOptions{SandboxRef: r.PathValue("ref")}
	if req.Name != nil {
		opts.Name = *req.Name
	}

	created, err := s.snapshots.Create(r.Context(), opts)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPISnapshot(created))
}

func (s *Server) handleListSandboxSnapshots(w http.ResponseWriter, r *http.Request) {
	sb, err := s.sandboxes.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.respondSnapshotList(w, r, snapshot.Filter{
		SandboxID: sb.ID,
		Limit:     pageLimit(r),
		Cursor:    r.URL.Query().Get("cursor"),
	})
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	filter := snapshot.Filter{Limit: pageLimit(r), Cursor: r.URL.Query().Get("cursor")}
	if ref := r.URL.Query().Get("sandbox"); ref != "" {
		sb, err := s.sandboxes.Get(r.Context(), ref)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		filter.SandboxID = sb.ID
	}
	s.respondSnapshotList(w, r, filter)
}

func (s *Server) respondSnapshotList(w http.ResponseWriter, r *http.Request, filter snapshot.Filter) {
	items, err := s.snapshots.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := apigen.SnapshotList{Items: make([]apigen.Snapshot, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, toAPISnapshot(item))
	}
	if len(items) == filter.Limit && len(items) > 0 {
		out.NextCursor = ptr(items[len(items)-1].ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	got, err := s.snapshots.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISnapshot(got))
}

func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := s.snapshots.Delete(r.Context(), r.PathValue("ref")); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreSandbox(w http.ResponseWriter, r *http.Request) {
	var req apigen.RestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: malformed JSON body", ErrInvalidRequest))
		return
	}
	if req.Snapshot == "" {
		s.writeError(w, r, fmt.Errorf("%w: snapshot is required", ErrInvalidRequest))
		return
	}

	restored, err := s.sandboxes.Restore(r.Context(), r.PathValue("ref"), req.Snapshot)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISandbox(restored))
}
