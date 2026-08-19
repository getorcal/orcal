package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

func (a *app) snapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot", Short: "Manage snapshots"}
	cmd.AddCommand(a.snapshotCreateCmd(), a.snapshotListCmd(), a.snapshotInspectCmd(), a.snapshotDeleteCmd())
	return cmd
}

func (a *app) snapshotCreateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create <sandbox>",
		Short: "Snapshot a sandbox's filesystem",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			created, err := a.client.CreateSnapshot(cmd.Context(), args[0], orcalclient.CreateSnapshotParams{Name: name})
			if err != nil {
				return err
			}
			return renderSnapshot(a.stdout, a.settings.Output, created)
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "snapshot name")
	return cmd
}

func (a *app) snapshotListCmd() *cobra.Command {
	var (
		sandboxRef string
		limit      int
		cursor     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshots",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			params := orcalclient.ListParams{Limit: limit, Cursor: cursor, Sandbox: sandboxRef}
			list, err := a.client.ListSnapshots(cmd.Context(), params)
			if err != nil {
				return err
			}
			return renderSnapshotList(a.stdout, a.settings.Output, list)
		}),
	}
	cmd.Flags().StringVar(&sandboxRef, "sandbox", "", "only snapshots of this sandbox")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	return cmd
}

func (a *app) snapshotInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <ref>",
		Short: "Show a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			got, err := a.client.GetSnapshot(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderSnapshotInspect(a.stdout, a.settings.Output, got)
		}),
	}
}

func (a *app) snapshotDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <ref>",
		Short: "Delete a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			if err := a.client.DeleteSnapshot(cmd.Context(), args[0]); err != nil {
				return err
			}
			_, writeErr := a.stdout.Write([]byte(args[0] + "\n"))
			return writeErr
		}),
	}
}

func (a *app) forkCmd() *cobra.Command {
	var (
		name        string
		cpuMillis   int
		memoryBytes int64
		pidsLimit   int
		envPairs    []string
		labelPairs  []string
	)
	cmd := &cobra.Command{
		Use:   "fork <snapshot>",
		Short: "Create a sandbox from a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			created, err := a.client.CreateSandbox(cmd.Context(), orcalclient.CreateSandboxParams{
				Name:        name,
				Snapshot:    args[0],
				CPUMillis:   cpuMillis,
				MemoryBytes: memoryBytes,
				PidsLimit:   pidsLimit,
				Env:         parsePairs(envPairs),
				Labels:      parsePairs(labelPairs),
			})
			if err != nil {
				return err
			}
			return renderSandbox(a.stdout, a.settings.Output, created)
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "sandbox name")
	cmd.Flags().IntVar(&cpuMillis, "cpu", 0, "CPU limit in millicores")
	cmd.Flags().Int64Var(&memoryBytes, "memory", 0, "memory limit in bytes")
	cmd.Flags().IntVar(&pidsLimit, "pids", 0, "maximum process count")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "environment variable as KEY=VALUE")
	cmd.Flags().StringArrayVar(&labelPairs, "label", nil, "label as KEY=VALUE")
	return cmd
}

func (a *app) restoreCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <sandbox> <snapshot>",
		Short: "Restore a sandbox to a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("%w: restore discards the sandbox's current filesystem; pass --yes to confirm", ErrConfirmationRequired)
			}
			restored, err := a.client.RestoreSandbox(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return renderSandbox(a.stdout, a.settings.Output, restored)
		}),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm that the current filesystem will be discarded")
	return cmd
}

func renderSnapshot(w io.Writer, format string, s *apigen.Snapshot) error {
	if format == "json" {
		return renderJSON(w, s)
	}
	fmt.Fprintf(w, "%s\n", s.Id)
	return nil
}

func renderSnapshotInspect(w io.Writer, format string, s *apigen.Snapshot) error {
	if format == "json" {
		return renderJSON(w, s)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "id:\t%s\n", s.Id)
	fmt.Fprintf(tw, "name:\t%s\n", derefOr(s.Name, "-"))
	fmt.Fprintf(tw, "sandbox:\t%s\n", s.SandboxId)
	fmt.Fprintf(tw, "parent:\t%s\n", derefOr(s.ParentId, "-"))
	fmt.Fprintf(tw, "image:\t%s\n", s.Image)
	fmt.Fprintf(tw, "size (apparent):\t%d\n", s.SizeBytes)
	fmt.Fprintf(tw, "created:\t%s\n", s.CreatedAt.Format(time.RFC3339))
	return tw.Flush()
}

func renderSnapshotList(w io.Writer, format string, list *apigen.SnapshotList) error {
	if format == "json" {
		return renderJSON(w, list)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSANDBOX\tPARENT\tSIZE (APPARENT)\tCREATED")
	for _, s := range list.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
			s.Id, derefOr(s.Name, "-"), s.SandboxId, derefOr(s.ParentId, "-"),
			s.SizeBytes, s.CreatedAt.Format(time.RFC3339))
	}
	return tw.Flush()
}

func derefOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}
