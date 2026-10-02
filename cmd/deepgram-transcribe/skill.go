package main

import (
	_ "embed"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

//go:embed SKILL.md
var embeddedSkill string

func newSkillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skill",
		Short: "Print the complete usage reference for agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := io.WriteString(cmd.OutOrStdout(), embeddedSkill); err != nil {
				return fmt.Errorf("writing skill: %w", err)
			}
			return nil
		},
	}
}
