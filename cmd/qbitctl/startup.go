package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func runDefault(cmd *cobra.Command, opts CLIOptions) error {
	if !interactiveWorkspace(cmd) {
		return cmd.Help()
	}
	setup, err := qbt.ReadSetup(opts)
	if err != nil {
		return result(cmd, err)
	}
	if setup.TUIDefault {
		return runTUI(cmd, opts)
	}
	if !setup.Ready() {
		finished, err := firstRunSetup(cmd, &opts, setup.Path)
		if finished || err != nil {
			return err
		}
	}
	return cmd.Help()
}

// firstRunSetup returns whether the chosen interface already fulfilled the
// invocation. The text wizard leaves the original command to run afterwards.
func firstRunSetup(cmd *cobra.Command, opts *CLIOptions, path string) (bool, error) {
	in := cmd.InOrStdin()
	out := cmd.ErrOrStderr()
	reader := bufio.NewReader(in)
	for {
		fmt.Fprint(out, "Choose setup  [1] Terminal UI  [2] Text  [Enter: 1] ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && strings.TrimSpace(choice) == "" {
				return true, nil
			}
			if !errors.Is(err, io.EOF) {
				return true, result(cmd, err)
			}
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "", "1", "tui":
			return true, runTUI(cmd, *opts)
		case "2", "text":
			savedPath, err := qbt.InteractiveConfigContext(cmd.Context(), reader, out, makeHiddenInput(in, out), path)
			if err != nil {
				return true, result(cmd, err)
			}
			opts.ConfigPath = savedPath
			opts.URL, opts.User, opts.Pass = "", "", ""
			return false, nil
		case "q", "quit", "cancel":
			return true, nil
		default:
			fmt.Fprintln(out, "Enter 1 for the TUI, 2 for text setup, or q to cancel.")
			if errors.Is(err, io.EOF) {
				return true, nil
			}
		}
	}
}

func newTUIDefaultCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:       "tui-default [on|off|status]",
		Short:     "Choose whether bare qbitctl opens the terminal UI",
		GroupID:   "setup",
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
