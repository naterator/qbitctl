//go:build !plan9

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/naterator/qbitctl/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:         "tui",
		Short:       "Open the interactive qBittorrent workspace",
		Long:        "Open the interactive qBittorrent workspace. Press Ctrl+P to find actions or open Config to set up a connection.",
		GroupID:     "view",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{stdinModeAnnotation: stdinInteractive},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd, *opts)
		},
	}
}

func runTUI(cmd *cobra.Command, opts CLIOptions) error {
	if !interactiveWorkspace(cmd) {
		return result(cmd, codedErr(exitBadArgs, "qbitctl tui requires an interactive terminal for input and output"))
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := tui.RunConfigurable(ctx, opts, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		return result(cmd, codedErr(exitActionFail, fmt.Sprintf("Terminal workspace failed: %v", err)))
	}
	return nil
}

func interactiveWorkspace(cmd *cobra.Command) bool {
	return tuiTerminal(cmd.InOrStdin()) && tuiTerminal(cmd.OutOrStdout())
}

func tuiTerminal(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(file.Fd())
}
