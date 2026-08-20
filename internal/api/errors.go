package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

type ErrorCode = apigen.ErrorBodyCode

var ErrInvalidRequest = errors.New("api: invalid request")
var ErrForbidden = errors.New("api: forbidden")

const (
	CodeInvalidRequest     ErrorCode = apigen.InvalidRequest
	CodeUnauthorized       ErrorCode = apigen.Unauthorized
	CodeForbidden          ErrorCode = apigen.Forbidden
	CodeTokenNotFound      ErrorCode = apigen.TokenNotFound
	CodeSandboxNotFound    ErrorCode = apigen.SandboxNotFound
	CodeExecNotFound       ErrorCode = apigen.ExecNotFound
	CodeSnapshotNotFound   ErrorCode = apigen.SnapshotNotFound
	CodePathNotFound       ErrorCode = apigen.PathNotFound
	CodeNameTaken          ErrorCode = apigen.NameTaken
	CodeInvalidState       ErrorCode = apigen.InvalidState
	CodeResourceExhausted  ErrorCode = apigen.ResourceExhausted
	CodeRuntimeUnavailable ErrorCode = apigen.RuntimeUnavailable
	CodeInternalError      ErrorCode = apigen.InternalError
)

// Order matters. Snapshot sentinels are matched before their sandbox equivalents because a
// fork or restore wraps both, and the first arm wins — moving these below sandbox.ErrNotFound
// would report a missing snapshot as a missing sandbox.
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
	case errors.Is(err, files.ErrPathNotFound):
		return http.StatusNotFound, CodePathNotFound
	case errors.Is(err, files.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, CodeResourceExhausted
	case errors.Is(err, files.ErrInvalidPath), errors.Is(err, files.ErrNotRegular),
		errors.Is(err, files.ErrUnsafeEntry), errors.Is(err, files.ErrNotDirectory):
		return http.StatusBadRequest, CodeInvalidRequest
	case errors.Is(err, ErrForbidden), errors.Is(err, auth.ErrScopeEscalation):
		return http.StatusForbidden, CodeForbidden
	case errors.Is(err, auth.ErrNotFound):
		return http.StatusNotFound, CodeTokenNotFound
	case errors.Is(err, auth.ErrLastAdminToken):
		return http.StatusConflict, CodeInvalidState
	case errors.Is(err, auth.ErrNameTaken):
		return http.StatusConflict, CodeNameTaken
	case errors.Is(err, auth.ErrInvalidScope), errors.Is(err, auth.ErrInvalidName):
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
		errors.Is(err, sandbox.ErrInvalidNetwork),
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
	details := map[string]any{"request_id": requestIDFrom(r.Context())}
	var scopeErr *missingScopeError
	if errors.As(err, &scopeErr) {
		details["required_scope"] = string(scopeErr.scope)
	}
	if code == CodeInternalError {
		s.logger.ErrorContext(r.Context(), "request failed", slog.String("error", err.Error()))
		message = "an internal error occurred"
	}
	writeJSON(w, status, apigen.Error{Error: apigen.ErrorBody{
		Code:    code,
		Message: message,
		Details: &details,
	}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
