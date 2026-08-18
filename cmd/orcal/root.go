package main

import (
	"fmt"
	"io"

	"github.com/getorcal/orcal/pkg/orcalclient"
	"github.com/spf13/cobra"
)

type app struct {
	settings settings
	client   *orcalclient.Client
	stdout   io.Writer
	stderr   io.Writer
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
	)

	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if code, ok := err.(execExitError); ok {
			return int(code)
		}
		printError(stderr, a.settings.Output == "json", err)
		return exitCode(err)
	}
	return exitOK
}

type execExitError int

func (e execExitError) Error() string { return fmt.Sprintf("exec exited with %d", int(e)) }
