package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func (a *app) eventsCmd() *cobra.Command {
	var (
		actor    string
		action   string
		resource string
		since    string
		limit    int
		cursor   string
	)
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Query the audit log",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			window, err := parseDuration(since)
			if err != nil {
				return err
			}
			params := orcalclient.ListEventsParams{
				Actor:      actor,
				Action:     action,
				ResourceID: resource,
				Limit:      limit,
				Cursor:     cursor,
			}
			if window > 0 {
				params.Since = time.Now().UTC().Add(-window)
			}
			list, err := a.client.ListEvents(cmd.Context(), params)
			if err != nil {
				return err
			}
			return renderEventList(a.stdout, a.settings.Output, list)
		}),
	}
	cmd.Flags().StringVar(&actor, "actor", "", "only events from this token id")
	cmd.Flags().StringVar(&action, "action", "", "only this action")
	cmd.Flags().StringVar(&resource, "resource", "", "only events touching this resource id")
	cmd.Flags().StringVar(&since, "since", "1h", "how far back to look, for example 1h or 7d")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum results")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	return cmd
}
