package main

import (
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestExecOutputExitCodeTreatsAFailedStateAsOrcalSelfFailure(t *testing.T) {
	zero, seventeen := 0, 17
	cases := []struct {
		name string
		e    orcalclient.OutputEvent
		want int
	}{
		{"clean exit code zero", orcalclient.OutputEvent{Event: "exit", State: "exited", ExitCode: &zero}, 0},
		{"clean exit code non-zero", orcalclient.OutputEvent{Event: "exit", State: "exited", ExitCode: &seventeen}, 17},
		{"failed with no exit code", orcalclient.OutputEvent{Event: "exit", State: "failed", ExitCode: nil}, exitSelf},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := execOutputExitCode(c.e); got != c.want {
				t.Errorf("execOutputExitCode(%+v) = %d, want %d", c.e, got, c.want)
			}
		})
	}
}
