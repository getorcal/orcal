package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

type app struct {
	settings settings
	client   *orcalclient.Client
	stdout   io.Writer
	stderr   io.Writer
	entered  bool
}

func (a *app) runE(fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a.entered = true
		return fn(cmd, args)
	}
}

func execute(args []string, stdout, stderr io.Writer) int {
	var (
		flagURL    string
		flagToken  string
		flagOutput string
		flagConfig string
	)
	a := &app{stdout: stdout, stderr: stderr}

	root := &cobra.Command{
		Use:           "orcal",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolveSettings(flagURL, flagToken, flagOutput, flagConfig)
			if err != nil {
				return err
			}
			a.settings = resolved
			a.client = orcalclient.New(resolved.URL, resolved.Token)
			return nil
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&flagURL, "url", "", "orcald base URL")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "API token")
	root.PersistentFlags().StringVar(&flagOutput, "output", "", "output format: human or json")
	root.PersistentFlags().StringVar(&flagConfig, "config", "", "config file path")

	root.AddCommand(
		a.createCmd(),
		a.listCmd(),
		a.inspectCmd(),
		a.lifecycleCmd("start", "Start a stopped sandbox"),
		a.lifecycleCmd("stop", "Stop a running sandbox"),
		a.lifecycleCmd("destroy", "Destroy a sandbox"),
		a.execCmd(),
		a.logsCmd(),
		a.snapshotCmd(),
		a.forkCmd(),
		a.restoreCmd(),
		a.cpCmd(),
		a.fileCmd(),
		a.tokenCmd(),
		a.eventsCmd(),
	)

	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var code execExitError
		if errors.As(err, &code) {
			return int(code)
		}
		_ = printError(stderr, a.settings.Output == "json", err)
		if !a.entered {
			return exitUsage
		}
		return exitCode(err)
	}
	return exitOK
}

type execExitError int

func (e execExitError) Error() string { return fmt.Sprintf("exec exited with %d", int(e)) }
