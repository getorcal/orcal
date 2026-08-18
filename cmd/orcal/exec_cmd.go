package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/getorcal/orcal/pkg/orcalclient"
	"github.com/spf13/cobra"
)

var errStopStream = errors.New("orcal: stop reading the output stream")

type streamOutcome struct {
	sawExit bool
	code    int
	offset  int64
}

func (a *app) execCmd() *cobra.Command {
	var (
		detach     bool
		workingDir string
		user       string
		envPairs   []string
	)
	cmd := &cobra.Command{
		Use:   "exec <ref> -- <command>...",
		Short: "Run a command in a sandbox",
		Args:  cobra.MinimumNArgs(2),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			started, err := a.client.CreateExec(cmd.Context(), args[0], orcalclient.CreateExecParams{
				Command:    args[1:],
				Env:        parsePairs(envPairs),
				WorkingDir: workingDir,
				User:       user,
			})
			if err != nil {
				return asSelfFailure(err)
			}
			if detach {
				return asSelfFailure(renderExec(a.stdout, a.settings.Output, started))
			}
			return asSelfFailure(a.streamAndExit(cmd.Context(), started.Id, 0))
		}),
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "return the exec id without streaming")
	cmd.Flags().StringVar(&workingDir, "workdir", "", "working directory inside the sandbox")
	cmd.Flags().StringVar(&user, "user", "", "user to run as")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "environment variable as KEY=VALUE")
	return cmd
}

func execOutputExitCode(e orcalclient.OutputEvent) int {
	if e.ExitCode != nil {
		return *e.ExitCode
	}
	return exitSelf
}

func (a *app) diagnostic(kind, message string) error {
	if a.settings.Output == "json" {
		return renderJSONLine(a.stderr, map[string]string{"event": kind, "message": message})
	}
	_, err := fmt.Fprintf(a.stderr, "orcal: %s\n", message)
	return err
}

func (a *app) streamAndExit(ctx context.Context, execID string, from int64) error {
	out, err := a.stream(ctx, execID, from, -1)
	if err != nil {
		return err
	}
	if !out.sawExit {
		if diagErr := a.diagnostic("stream_ended", fmt.Sprintf(
			"output stream ended without an exit event; the command's exit status is unknown, treating as an orcal failure (exit %d)",
			exitSelf)); diagErr != nil {
			return diagErr
		}
		return execExitError(exitSelf)
	}
	if out.code != 0 {
		return execExitError(out.code)
	}
	return nil
}

func (a *app) stream(ctx context.Context, execID string, from, stopAt int64) (streamOutcome, error) {
	out := streamOutcome{offset: from}
	err := a.client.StreamOutput(ctx, execID, from, func(e orcalclient.OutputEvent) error {
		switch e.Event {
		case "output":
			target := a.stdout
			if e.Stream == "stderr" {
				target = a.stderr
			}
			if _, writeErr := target.Write(e.Data); writeErr != nil {
				return writeErr
			}
			out.offset = e.Offset
		case "gap":
			out.offset = e.Offset
			if diagErr := a.diagnostic("gap",
				"output gap - the daemon restarted during this exec, output is incomplete"); diagErr != nil {
				return diagErr
			}
		case "exit":
			out.sawExit = true
			if e.Truncated {
				if diagErr := a.diagnostic("truncated", "output truncated at the configured limit"); diagErr != nil {
					return diagErr
				}
			}
			out.code = execOutputExitCode(e)
			if e.ExitCode == nil {
				return a.diagnostic("exec_failed", fmt.Sprintf(
					"exec ended in state %q without an exit code; treating as an orcal failure (exit %d)", e.State, exitSelf))
			}
			return nil
		}
		if stopAt >= 0 && out.offset >= stopAt {
			return errStopStream
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopStream) {
		return out, err
	}
	return out, nil
}
