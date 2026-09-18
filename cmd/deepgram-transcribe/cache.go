package main

import (
	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// newCacheCmd builds the cache subject.
func newCacheCmd(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local response cache",
	}

	cmd.AddCommand(
		newCacheClearCmd(g),
		newCacheStatusCmd(g),
	)

	return cmd
}

// newCacheStatusCmd builds the cache status verb.
func newCacheStatusCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the cache location and how many responses are stored",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			records, err := deepgram.ListJobRecords(g.resolvedCacheDir())
			if err != nil {
				return err
			}

			out.Fields().
				Add("Directory", "%s", g.resolvedCacheDir()).
				Add("Cached responses", "%d", len(records)).
				Render()

			return nil
		},
	}
}

// newCacheClearCmd builds the cache clear verb.
func newCacheClearCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Delete all cached responses and job history",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			removed, err := deepgram.ClearCache(g.resolvedCacheDir())
			if err != nil {
				return err
			}

			out.Success("Deleted %d cached response(s) from %s.", removed, g.resolvedCacheDir())

			return nil
		},
	}
}
