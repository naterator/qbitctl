//go:build !plan9

package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/naterator/qbitctl/internal/tui"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func newTUICmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "tui",
		Short:   "Open the interactive qBittorrent workspace",
		Long:    "Open the interactive qBittorrent workspace with your existing qbitctl config. Press Ctrl+P to find actions.",
		GroupID: "view",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, output := cmd.InOrStdin(), cmd.OutOrStdout()
			if !tuiTerminal(input) || !tuiTerminal(output) {
				return result(codedErr(exitBadArgs, "qbitctl tui requires an interactive terminal for input and output"))
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			app, err := qbt.NewClientWithOutput(ctx, opts, io.Discard, io.Discard)
			if err != nil {
				return result(err)
			}
			defer app.Logout()
			if err := tui.Run(ctx, app, input, output); err != nil {
				return result(codedErr(exitActionFail, fmt.Sprintf("Terminal workspace failed: %v", err)))
			}
			return nil
		},
	}
}

func tuiTerminal(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(file.Fd())
}
