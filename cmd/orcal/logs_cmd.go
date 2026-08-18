package main

import "github.com/spf13/cobra"

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
			err := a.streamAndExit(cmd, args[0], from)
			if _, isExit := err.(execExitError); isExit {
				return nil
			}
			return err
		}),
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "keep streaming until the exec finishes")
	cmd.Flags().Int64Var(&from, "from", 0, "resume from this byte offset")
	return cmd
}
