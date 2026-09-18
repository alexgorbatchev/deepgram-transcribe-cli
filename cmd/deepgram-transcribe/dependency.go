package main

import (
	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/deps"
)

// newDependencyCmd builds the dependency subject.
func newDependencyCmd(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dependency",
		Short: "Manage the other programs this tool needs",
	}

	cmd.AddCommand(
		newDependencyInstallCmd(g),
		newDependencyListCmd(g),
		newDependencyUpdateCmd(g),
	)

	return cmd
}

// newDependencyListCmd builds the dependency list verb.
func newDependencyListCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the programs this tool needs and whether they are ready",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			// Verify reports an error when anything is unsatisfied, which is the
			// answer this command exists to show rather than a reason to stop.
			reports, _ := g.newDependencyManager().Verify(cmd.Context())

			rows := make([][]string, 0, len(reports))
			for _, report := range reports {
				installed := report.DetectedVersion
				if installed == "" {
					installed = "none"
				}

				rows = append(rows, []string{
					report.Name,
					report.MinVersion,
					installed,
					deps.Status(report),
				})
			}

			return out.Table([]string{"Program", "Needs", "Installed", "Status"}, rows)
		},
	}
}

// newDependencyInstallCmd builds the dependency install verb.
func newDependencyInstallCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install anything that is missing or too old",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			installed, err := g.newDependencyManager().InstallUnsatisfied(cmd.Context())
			if err != nil {
				return cliout.WithHint(err, "Install it yourself, then run `deepgram-transcribe dependency list` to check.")
			}

			if len(installed) == 0 {
				out.Success("Everything this tool needs is already installed.")
				return nil
			}

			for _, name := range installed {
				out.Success("Installed %s.", name)
			}

			return nil
		},
	}
}

// newDependencyUpdateCmd builds the dependency update verb.
func newDependencyUpdateCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update the programs this tool needs to their newest versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())

			updated, err := g.newDependencyManager().UpdateAll(cmd.Context())
			if err != nil {
				return cliout.WithHint(err, "Update it yourself, then run `deepgram-transcribe dependency list` to check.")
			}

			for _, name := range updated {
				out.Success("Updated %s.", name)
			}

			return nil
		},
	}
}
