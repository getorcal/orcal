package main

import (
	"fmt"

	"github.com/getorcal/orcal/pkg/orcalclient"
	"github.com/spf13/cobra"
)

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
				return err
			}
			if detach {
				return renderExec(a.stdout, a.settings.Output, started)
			}
			return a.streamAndExit(cmd, started.Id, 0)
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

func (a *app) streamAndExit(cmd *cobra.Command, execID string, from int64) error {
	code := 0
	err := a.client.StreamOutput(cmd.Context(), execID, from, func(e orcalclient.OutputEvent) error {
		switch e.Event {
		case "output":
			target := a.stdout
			if e.Stream == "stderr" {
				target = a.stderr
			}
			_, writeErr := target.Write(e.Data)
			return writeErr
		case "gap":
			return a.diagnostic("gap", "output gap - the daemon restarted during this exec, output is incomplete")
		case "exit":
			if e.Truncated {
				if err := a.diagnostic("truncated", "output truncated at the configured limit"); err != nil {
					return err
				}
			}
			code = execOutputExitCode(e)
			if e.ExitCode == nil {
				return a.diagnostic("exec_failed", fmt.Sprintf(
					"exec ended in state %q without an exit code; treating as an orcal failure (exit %d)", e.State, exitSelf))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return execExitError(code)
	}
	return nil
}
