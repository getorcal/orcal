package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/exec"
)

const heartbeatInterval = 30 * time.Second

func (s *Server) handleCreateExec(w http.ResponseWriter, r *http.Request) {
	sb, err := s.sandboxes.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	var req apigen.CreateExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: malformed JSON body", ErrInvalidRequest))
		return
	}
	if len(req.Command) == 0 {
		s.writeError(w, r, fmt.Errorf("%w: command is required", ErrInvalidRequest))
		return
	}

	opts := exec.CreateOptions{SandboxID: sb.ID, Command: req.Command}
	if req.Env != nil {
		opts.Env = *req.Env
	}
	if req.WorkingDir != nil {
		opts.WorkingDir = *req.WorkingDir
	}
	if req.User != nil {
		opts.User = *req.User
	}

	created, err := s.execs.Create(r.Context(), opts)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "exec"
		a.resourceID = created.ID
	})
	writeJSON(w, http.StatusCreated, toAPIExec(created))
}

func (s *Server) handleListExecs(w http.ResponseWriter, r *http.Request) {
	sb, err := s.sandboxes.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	limit := pageLimit(r)
	items, err := s.execs.ListBySandbox(r.Context(), sb.ID, exec.Filter{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	out := apigen.ExecList{Items: make([]apigen.Exec, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, toAPIExec(item))
	}
	if len(items) == limit && len(items) > 0 {
		out.NextCursor = ptr(items[len(items)-1].ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetExec(w http.ResponseWriter, r *http.Request) {
	got, err := s.execs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIExec(got))
}

func (s *Server) handleExecOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.execs.Get(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}

	from := int64(0)
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed < 0 {
			s.writeError(w, r, fmt.Errorf("%w: from must be a non-negative integer", ErrInvalidRequest))
			return
		}
		from = parsed
	}

	stream, ok := newSSEWriter(w)
	if !ok {
		s.writeError(w, r, errors.New("streaming is not supported by this connection"))
		return
	}

	offset := from
	for {
		wake := s.execs.Broadcaster().Wait(id)

		next, drainErr := s.drain(stream, id, offset)
		if drainErr != nil {
			return
		}
		offset = next

		current, err := s.execs.Get(r.Context(), id)
		if err != nil {
			return
		}
		if current.State != exec.StateRunning {
			if _, err := s.drain(stream, id, offset); err != nil {
				return
			}
			_ = stream.send("exit", map[string]any{
				"state":     string(current.State),
				"exit_code": current.ExitCode,
				"truncated": current.Truncated,
			})
			return
		}

		select {
		case <-wake:
		case <-r.Context().Done():
			return
		case <-time.After(heartbeatInterval):
			if err := stream.heartbeat(); err != nil {
				return
			}
		}
	}
}

func (s *Server) drain(stream *sseWriter, id string, offset int64) (int64, error) {
	records, err := exec.ReadRecords(s.execs.LogPath(id), offset)
	if err != nil {
		return offset, err
	}
	for _, record := range records {
		event := "output"
		payload := map[string]any{"offset": record.Offset}
		switch record.Stream {
		case exec.LogGap:
			event = "gap"
		case exec.LogStderr:
			payload["stream"] = "stderr"
			payload["data"] = base64.StdEncoding.EncodeToString(record.Data)
		default:
			payload["stream"] = "stdout"
			payload["data"] = base64.StdEncoding.EncodeToString(record.Data)
		}
		if err := stream.send(event, payload); err != nil {
			return record.Offset, err
		}
		offset = record.Offset
	}
	return offset, nil
}
