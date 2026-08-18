package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

func TestClassifyMapsEveryDomainSentinel(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{fmt.Errorf("wrapped: %w", sandbox.ErrNotFound), http.StatusNotFound, CodeSandboxNotFound},
		{fmt.Errorf("wrapped: %w", exec.ErrNotFound), http.StatusNotFound, CodeExecNotFound},
		{fmt.Errorf("wrapped: %w", sandbox.ErrNameTaken), http.StatusConflict, CodeNameTaken},
		{fmt.Errorf("wrapped: %w", sandbox.ErrInvalidState), http.StatusConflict, CodeInvalidState},
		{fmt.Errorf("wrapped: %w", sandbox.ErrInvalidName), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", sandbox.ErrNameLooksLikeID), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", sandbox.ErrInvalidImage), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", ErrInvalidRequest), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", sandbox.ErrResourceExhausted), http.StatusTooManyRequests, CodeResourceExhausted},
		{fmt.Errorf("wrapped: %w", runtime.ErrUnavailable), http.StatusServiceUnavailable, CodeRuntimeUnavailable},
		{fmt.Errorf("wrapped: %w", runtime.ErrNotFound), http.StatusConflict, CodeInvalidState},
		{fmt.Errorf("wrapped: %w", runtime.ErrConflict), http.StatusConflict, CodeInvalidState},
		{fmt.Errorf("wrapped: %w", runtime.ErrInvalidSpec), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", snapshot.ErrNotFound), http.StatusNotFound, CodeSnapshotNotFound},
		{fmt.Errorf("wrapped: %w", snapshot.ErrHasChildren), http.StatusConflict, CodeInvalidState},
		{fmt.Errorf("wrapped: %w", snapshot.ErrBackingImageMissing), http.StatusConflict, CodeInvalidState},
		{fmt.Errorf("wrapped: %w", snapshot.ErrNameTaken), http.StatusConflict, CodeNameTaken},
		{fmt.Errorf("wrapped: %w", snapshot.ErrInvalidName), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("wrapped: %w", snapshot.ErrNameLooksLikeID), http.StatusBadRequest, CodeInvalidRequest},
		{fmt.Errorf("something unexpected"), http.StatusInternalServerError, CodeInternalError},
	}
	for _, c := range cases {
		status, code := classify(c.err)
		if status != c.wantStatus || code != c.wantCode {
			t.Errorf("classify(%v) = %d/%s, want %d/%s", c.err, status, code, c.wantStatus, c.wantCode)
		}
	}
}

func TestClassifyPrefersSandboxNotFoundOverRuntimeNotFound(t *testing.T) {
	status, code := classify(fmt.Errorf("%w", sandbox.ErrNotFound))
	if status != http.StatusNotFound || code != CodeSandboxNotFound {
		t.Errorf("classify(sandbox.ErrNotFound) = %d/%s, want 404/sandbox_not_found", status, code)
	}
}
