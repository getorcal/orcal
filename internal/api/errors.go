package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

type ErrorCode = apigen.ErrorBodyCode

var ErrInvalidRequest = errors.New("api: invalid request")

const (
	CodeInvalidRequest     ErrorCode = apigen.InvalidRequest
	CodeUnauthorized       ErrorCode = apigen.Unauthorized
	CodeSandboxNotFound    ErrorCode = apigen.SandboxNotFound
	CodeExecNotFound       ErrorCode = apigen.ExecNotFound
	CodeSnapshotNotFound   ErrorCode = apigen.SnapshotNotFound
	CodeNameTaken          ErrorCode = apigen.NameTaken
	CodeInvalidState       ErrorCode = apigen.InvalidState
	CodeResourceExhausted  ErrorCode = apigen.ResourceExhausted
	CodeRuntimeUnavailable ErrorCode = apigen.RuntimeUnavailable
	CodeInternalError      ErrorCode = apigen.InternalError
)

func classify(err error) (int, ErrorCode) {
	switch {
	case errors.Is(err, snapshot.ErrNotFound):
		return http.StatusNotFound, CodeSnapshotNotFound
	case errors.Is(err, snapshot.ErrHasChildren):
		return http.StatusConflict, CodeInvalidState
	case errors.Is(err, snapshot.ErrNameTaken):
		return http.StatusConflict, CodeNameTaken
	case errors.Is(err, snapshot.ErrInvalidName), errors.Is(err, snapshot.ErrNameLooksLikeID):
		return http.StatusBadRequest, CodeInvalidRequest
	case errors.Is(err, sandbox.ErrNotFound):
		return http.StatusNotFound, CodeSandboxNotFound
	case errors.Is(err, exec.ErrNotFound):
		return http.StatusNotFound, CodeExecNotFound
	case errors.Is(err, sandbox.ErrNameTaken):
		return http.StatusConflict, CodeNameTaken
	case errors.Is(err, sandbox.ErrInvalidState):
		return http.StatusConflict, CodeInvalidState
	case errors.Is(err, sandbox.ErrInvalidName), errors.Is(err, sandbox.ErrNameLooksLikeID),
		errors.Is(err, sandbox.ErrInvalidImage), errors.Is(err, sandbox.ErrInvalidResources),
		errors.Is(err, ErrInvalidRequest), errors.Is(err, runtime.ErrInvalidSpec):
		return http.StatusBadRequest, CodeInvalidRequest
	case errors.Is(err, sandbox.ErrResourceExhausted):
		return http.StatusTooManyRequests, CodeResourceExhausted
	case errors.Is(err, runtime.ErrUnavailable):
		return http.StatusServiceUnavailable, CodeRuntimeUnavailable
	case errors.Is(err, runtime.ErrNotFound), errors.Is(err, runtime.ErrConflict):
		return http.StatusConflict, CodeInvalidState
	default:
		return http.StatusInternalServerError, CodeInternalError
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := classify(err)
	message := err.Error()
	if code == CodeInternalError {
		s.logger.ErrorContext(r.Context(), "request failed", slog.String("error", err.Error()))
		message = "an internal error occurred"
	}
	writeJSON(w, status, apigen.Error{Error: apigen.ErrorBody{
		Code:    code,
		Message: message,
		Details: &map[string]any{"request_id": requestIDFrom(r.Context())},
	}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
