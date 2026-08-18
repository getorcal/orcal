package main

import (
	"errors"
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestExitCodeMapsAPIErrorsToStableCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&orcalclient.APIError{StatusCode: 404, Code: "sandbox_not_found"}, 3},
		{&orcalclient.APIError{StatusCode: 404, Code: "exec_not_found"}, 3},
		{&orcalclient.APIError{StatusCode: 401, Code: "unauthorized"}, 4},
		{&orcalclient.APIError{StatusCode: 400, Code: "invalid_request"}, 2},
		{&orcalclient.APIError{StatusCode: 409, Code: "invalid_state"}, 1},
		{&orcalclient.APIError{StatusCode: 503, Code: "runtime_unavailable"}, 1},
		{errors.New("connection refused"), 1},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
