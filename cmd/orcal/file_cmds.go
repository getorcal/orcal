package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/internal/apigen"
)

func (a *app) fileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "file", Short: "Inspect files inside a sandbox"}
	cmd.AddCommand(a.fileListCmd(), a.fileStatCmd())
	return cmd
}

func (a *app) fileListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls <sandbox> [path]",
		Short: "List files in a sandbox directory",
		Args:  cobra.RangeArgs(1, 2),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			path := "/"
			if len(args) == 2 {
				path = args[1]
			}
			list, err := a.client.ListFiles(cmd.Context(), args[0], path)
			if err != nil {
				return err
			}
			return renderFileList(a.stdout, a.settings.Output, list)
		}),
	}
	return cmd
}

func (a *app) fileStatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stat <sandbox> <path>",
		Short: "Show metadata for a file inside a sandbox",
		Args:  cobra.ExactArgs(2),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			info, err := a.client.StatFile(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return renderFileInfo(a.stdout, a.settings.Output, info)
		}),
	}
}

func renderFileList(w io.Writer, format string, list *apigen.FileList) error {
	if format == "json" {
		return renderJSON(w, list)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODE\tSIZE\tMTIME\tNAME")
	for _, item := range list.Items {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", item.Mode, item.Size, item.Mtime.Format(time.RFC3339), item.Name)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if list.Truncated {
		_, err := fmt.Fprintln(w, "(truncated)")
		return err
	}
	return nil
}

func renderFileInfo(w io.Writer, format string, info *apigen.FileInfo) error {
	if format == "json" {
		return renderJSON(w, info)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "name:\t%s\n", info.Name)
	fmt.Fprintf(tw, "mode:\t%s\n", info.Mode)
	fmt.Fprintf(tw, "size:\t%d\n", info.Size)
	fmt.Fprintf(tw, "is_dir:\t%t\n", info.IsDir)
	fmt.Fprintf(tw, "mtime:\t%s\n", info.Mtime.Format(time.RFC3339))
	fmt.Fprintf(tw, "link_target:\t%s\n", derefOr(info.LinkTarget, "-"))
	return tw.Flush()
}
