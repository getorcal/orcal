package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func parseDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	withoutD, hasDSuffix := strings.CutSuffix(raw, "d")
	if hasDSuffix {
		days, err := strconv.Atoi(withoutD)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("%w: %q is not a whole number of days", ErrUsage, raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a duration", ErrUsage, raw)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%w: %q must not be negative", ErrUsage, raw)
	}
	return parsed, nil
}

func (a *app) tokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage API tokens"}
	cmd.AddCommand(a.tokenCreateCmd(), a.tokenListCmd(), a.tokenRevokeCmd())
	return cmd
}

func (a *app) tokenCreateCmd() *cobra.Command {
	var (
		name    string
		scopes  []string
		expires string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Mint a token; the value is printed once and never again",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			ttl, err := parseDuration(expires)
			if err != nil {
				return err
			}
			created, err := a.client.CreateToken(cmd.Context(), orcalclient.CreateTokenParams{
				Name:             name,
				Scopes:           scopes,
				ExpiresInSeconds: int64(ttl / time.Second),
			})
			if err != nil {
				return err
			}
			return renderCreatedToken(a.stdout, a.stderr, a.settings.Output, created)
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "token name")
	cmd.Flags().StringArrayVar(&scopes, "scope", nil, "scope to grant; repeat for several")
	cmd.Flags().StringVar(&expires, "expires", "", "lifetime, for example 90d or 12h")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func (a *app) tokenListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tokens",
		RunE: a.runE(func(cmd *cobra.Command, _ []string) error {
			list, err := a.client.ListTokens(cmd.Context())
			if err != nil {
				return err
			}
			return renderTokenList(a.stdout, a.settings.Output, list)
		}),
	}
}

func (a *app) tokenRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a token",
		Args:  cobra.ExactArgs(1),
		RunE: a.runE(func(cmd *cobra.Command, args []string) error {
			if err := a.client.RevokeToken(cmd.Context(), args[0]); err != nil {
				return err
			}
			if a.settings.Output == "json" {
				return renderJSONLine(a.stdout, map[string]any{"id": args[0]})
			}
			_, writeErr := fmt.Fprintln(a.stdout, args[0])
			return writeErr
		}),
	}
}
