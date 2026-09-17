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
		Short: "Manage the transcripts saved on this computer",
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
		Short: "Show where transcripts are saved and how many are kept",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			records, err := deepgram.ListJobRecords(g.resolvedCacheDir())
			if err != nil {
				return err
			}

			out.Fields().
				Add("Folder", "%s", g.resolvedCacheDir()).
				Add("Saved transcripts", "%d", len(records)).
				Render()

			return nil
		},
	}
}

// newCacheClearCmd builds the cache clear verb.
func newCacheClearCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Delete every saved transcript and its history entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			removed, err := deepgram.ClearCache(g.resolvedCacheDir())
			if err != nil {
				return err
			}

			out.Success("Deleted %d saved transcript(s) from %s.", removed, g.resolvedCacheDir())

			return nil
		},
	}
}
