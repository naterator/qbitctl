package main

import "github.com/spf13/cobra"

func newTUICmd(_ *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "tui",
		Short:   "Open the interactive qBittorrent workspace",
		GroupID: "view",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return result(codedErr(exitBadArgs, "The interactive terminal workspace is unavailable on Plan 9"))
		},
	}
}
