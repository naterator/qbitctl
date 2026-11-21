package main

import "github.com/spf13/cobra"

func newTUICmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "tui",
		Short:   "Open the interactive qBittorrent workspace",
		GroupID: "view",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd, *opts)
		},
	}
}

func interactiveWorkspace(*cobra.Command) bool { return false }

func runTUI(cmd *cobra.Command, _ CLIOptions) error {
	return result(cmd, codedErr(exitBadArgs, "The interactive terminal workspace is unavailable on Plan 9"))
}
