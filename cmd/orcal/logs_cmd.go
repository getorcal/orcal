package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

const logsReconnectAttempts = 3

func (a *app) logsCmd() *cobra.Command {
	var (
		follow bool
		from   int64
	)
	cmd := &cobra.Command{
		Use:   "logs <exec-id>",
		Short: "Stream the output of an exec",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			if follow {
				return a.followLogs(cmd.Context(), args[0], from)
			}
			return a.readAvailableLogs(cmd.Context(), args[0], from)
		}),
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "keep streaming until the exec finishes")
	cmd.Flags().Int64Var(&from, "from", 0, "resume from this byte offset")
	return cmd
}

func (a *app) readAvailableLogs(ctx context.Context, execID string, from int64) error {
	current, err := a.client.GetExec(ctx, execID)
	if err != nil {
		return err
	}
	if current.OutputBytes <= from {
		return nil
	}
	_, err = a.stream(ctx, execID, from, current.OutputBytes)
	return err
}

func (a *app) followLogs(ctx context.Context, execID string, from int64) error {
	offset := from
	for attempt := 0; ; attempt++ {
		out, err := a.stream(ctx, execID, offset, -1)
		if err != nil {
			return err
		}
		offset = out.offset
		if out.sawExit {
			return nil
		}
		if attempt >= logsReconnectAttempts {
			return a.diagnostic("stream_ended", fmt.Sprintf(
				"output stream ended without an exit event after %d reconnect attempts; the output above may be incomplete",
				logsReconnectAttempts))
		}
		if err := a.diagnostic("reconnect", fmt.Sprintf(
			"output stream ended without an exit event; reconnecting from offset %d", offset)); err != nil {
			return err
		}
	}
}
