package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/getorcal/orcal/internal/apigen"
)

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	ref, path := r.PathValue("ref"), r.URL.Query().Get("path")
	rc, info, err := s.files.Read(r.Context(), ref, path)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rc.Close()

	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "sandbox"
		a.resourceID = ref
		a.details = map[string]any{"path": path}
	})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("Last-Modified", info.ModTime.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	ref, path := r.PathValue("ref"), r.URL.Query().Get("path")
	if err := s.files.Write(r.Context(), ref, path, r.Body); err != nil {
		s.writeError(w, r, err)
		return
	}
	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "sandbox"
		a.resourceID = ref
		a.details = map[string]any{"path": path}
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatFile(w http.ResponseWriter, r *http.Request) {
	info, err := s.files.Stat(r.Context(), r.PathValue("ref"), r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIFileInfo(info))
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	listing, err := s.files.List(r.Context(), r.PathValue("ref"), r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := apigen.FileList{Items: make([]apigen.FileInfo, 0, len(listing.Items)), Truncated: listing.Truncated}
	for _, item := range listing.Items {
		out.Items = append(out.Items, toAPIFileInfo(item))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDownloadArchive(w http.ResponseWriter, r *http.Request) {
	ref, path := r.PathValue("ref"), r.URL.Query().Get("path")
	rc, err := s.files.DownloadArchive(r.Context(), ref, path)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rc.Close()

	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "sandbox"
		a.resourceID = ref
		a.details = map[string]any{"path": path}
	})
	w.Header().Set("Content-Type", "application/x-tar")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleUploadArchive(w http.ResponseWriter, r *http.Request) {
	ref, path := r.PathValue("ref"), r.URL.Query().Get("path")
	if err := s.files.UploadArchive(r.Context(), ref, path, r.Body); err != nil {
		s.writeError(w, r, err)
		return
	}
	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "sandbox"
		a.resourceID = ref
		a.details = map[string]any{"path": path}
	})
	w.WriteHeader(http.StatusNoContent)
}
