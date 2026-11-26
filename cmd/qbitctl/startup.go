package main

import (
	"fmt"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// These seams keep the bare-command startup policy testable without claiming
// that a test process owns an interactive terminal or starting Bubble Tea.
var (
	isInteractive = interactiveWorkspace
	runTUIFunc    = runTUI
)

func runDefault(cmd *cobra.Command, opts CLIOptions) error {
	if !isInteractive(cmd) {
		return cmd.Help()
	}
	setup, err := qbt.ReadSetup(opts)
	if err != nil {
		return result(cmd, err)
	}
	if setup.TUIDefault {
		return runTUIFunc(cmd, opts)
	}
	return cmd.Help()
}

func newTUIDefaultCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:       "tui-default [on|off|status]",
		Short:     "Choose whether bare qbitctl opens the terminal UI",
		GroupID:   "setup",
		Hidden:    true,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off", "status"},
		RunE: func(cmd *cobra.Command, args []string) error {
			action := "status"
			if len(args) > 0 {
				action = args[0]
			}
			if action != "on" && action != "off" && action != "status" {
				return result(cmd, codedErr(exitBadArgs, "Use tui-default on, off, or status"))
			}

			setup, err := qbt.ReadSetup(*opts)
			if err != nil {
				return result(cmd, err)
			}
			enabled := setup.TUIDefault
			if action != "status" {
				enabled = action == "on"
				if err := qbt.SetTUIDefault(setup.Path, enabled); err != nil {
					return result(cmd, err)
				}
			}
			state := "off"
			if enabled {
				state = "on"
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), state)
			return err
		},
	}
}
