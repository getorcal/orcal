package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func (a *app) createCmd() *cobra.Command {
	var (
		name        string
		image       string
		cpuMillis   int
		memoryBytes int64
		pidsLimit   int
		envPairs    []string
		labelPairs  []string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and start a sandbox",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			params := orcalclient.CreateSandboxParams{
				Name:        name,
				Image:       image,
				CPUMillis:   cpuMillis,
				MemoryBytes: memoryBytes,
				PidsLimit:   pidsLimit,
				Env:         parsePairs(envPairs),
				Labels:      parsePairs(labelPairs),
			}
			created, err := a.client.CreateSandbox(cmd.Context(), params)
			if err != nil {
				return err
			}
			return renderSandbox(a.stdout, a.settings.Output, created)
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "sandbox name")
	cmd.Flags().StringVar(&image, "image", "", "container image")
	cmd.Flags().IntVar(&cpuMillis, "cpu", 0, "CPU limit in millicores")
	cmd.Flags().Int64Var(&memoryBytes, "memory", 0, "memory limit in bytes")
	cmd.Flags().IntVar(&pidsLimit, "pids", 0, "maximum process count")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "environment variable as KEY=VALUE")
	cmd.Flags().StringArrayVar(&labelPairs, "label", nil, "label as KEY=VALUE")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func (a *app) listCmd() *cobra.Command {
	var (
		limit      int
		cursor     string
		state      string
		labelPairs []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List sandboxes",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			list, err := a.client.ListSandboxes(cmd.Context(), orcalclient.ListParams{
				Limit:  limit,
				Cursor: cursor,
				State:  state,
				Labels: parsePairs(labelPairs),
			})
			if err != nil {
				return err
			}
			return renderSandboxList(a.stdout, a.settings.Output, list)
		}),
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().StringVar(&state, "state", "", "filter by state")
	cmd.Flags().StringArrayVar(&labelPairs, "label", nil, "filter by label KEY=VALUE")
	return cmd
}

func (a *app) inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <ref>",
		Short: "Show a sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			got, err := a.client.GetSandbox(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderSandboxInspect(a.stdout, a.settings.Output, got)
		}),
	}
}

func (a *app) lifecycleCmd(verb, short string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <ref>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			var (
				result any
				err    error
			)
			switch verb {
			case "start":
				result, err = a.client.StartSandbox(cmd.Context(), args[0])
			case "stop":
				result, err = a.client.StopSandbox(cmd.Context(), args[0])
			case "destroy":
				result, err = a.client.DestroySandbox(cmd.Context(), args[0])
			}
			if err != nil {
				return err
			}
			if a.settings.Output == "json" {
				return renderJSON(a.stdout, result)
			}
			_, writeErr := a.stdout.Write([]byte(args[0] + "\n"))
			return writeErr
		}),
	}
}

func parsePairs(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if found {
			out[key] = value
		}
	}
	return out
}
