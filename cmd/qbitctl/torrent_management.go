package main

import "github.com/spf13/cobra"

// newRecheckCmd creates the bulk torrent verification command. Register it in
// newRootCmd alongside the other torrent control commands.
func newRecheckCmd(opts *CLIOptions) *cobra.Command {
	return newHashCmd(opts, "recheck <hash...>", "Verify torrent data", []string{"rc"}, (*App).RecheckTorrent)
}

// newReannounceCmd creates the bulk tracker reannounce command. Register it
// in newRootCmd alongside the other torrent control commands.
func newReannounceCmd(opts *CLIOptions) *cobra.Command {
	return newHashCmd(opts, "reannounce <hash...>", "Force a tracker reannounce", []string{"ra"}, (*App).ReannounceTorrent)
}

// newQueueCmd creates queue-priority subcommands. Register it in newRootCmd;
// each subcommand resolves all prefixes once and sends one bulk request.
func newQueueCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "queue",
		Aliases: []string{"q"},
		Short:   "Change torrent queue order",
		GroupID: "control",
		Args:    cobra.NoArgs,
	}
	children := []*cobra.Command{
		newHashCmd(opts, "up <hash...>", "Move torrents up one queue position", []string{"u"}, (*App).QueueUpTorrent),
		newHashCmd(opts, "down <hash...>", "Move torrents down one queue position", []string{"d"}, (*App).QueueDownTorrent),
		newHashCmd(opts, "top <hash...>", "Move torrents to the top of the queue", []string{"t"}, (*App).QueueTopTorrent),
		newHashCmd(opts, "bottom <hash...>", "Move torrents to the bottom of the queue", []string{"b"}, (*App).QueueBottomTorrent),
	}
	for _, child := range children {
		child.GroupID = ""
	}
	cmd.AddCommand(children...)
	return cmd
}

// newRenameCmd creates the single-torrent rename command. The name comes first
// to match qbitctl's existing value-oriented set syntax:
//
//	qbitctl rename "Ubuntu Desktop" <hash>
//
// Register it in newRootCmd alongside set and move.
func newRenameCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "rename <name> <hash>",
		Aliases: []string{"rn"},
		Short:   "Rename one torrent",
		GroupID: "change",
		Args:    cobra.ExactArgs(2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 1 {
				return completeTorrentHashes(opts, nil, toComplete)
			}
			return cobra.NoFileCompletions(nil, nil, toComplete)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[1])
			if err != nil {
				return err
			}
			if err := app.RenameTorrent(hash, args[0]); err != nil {
				return err
			}
			return nil
		}),
	}
}
