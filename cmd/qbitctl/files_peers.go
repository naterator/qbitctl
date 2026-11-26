package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// newFilesCmd is registered by the root command. The list form intentionally
// keeps the hash first, matching `show` and `get`, while mutations are grouped
// under explicit subcommands so a path can never be mistaken for a priority.
func newFilesCmd(opts *CLIOptions) *cobra.Command {
	jsonOutput := false
	cmd := &cobra.Command{
		Use:     "files <hash>",
		Short:   "List torrent files",
		GroupID: "view",
		Args:    authenticatedArgs(opts, cobra.ExactArgs(1)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			return completeSingleTorrentHashArgs(opts)(cmd, args, toComplete)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			files, err := app.FetchTorrentFiles(hash)
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(app.Stdout).Encode(files)
			}
			renderTorrentFiles(app.Stdout, app, files)
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output torrent files as JSON")

	priority := &cobra.Command{
		Use:     "priority <hash> <priority> <file-index...>",
		Aliases: []string{"prio"},
		Short:   "Set priority for selected torrent files",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(3)(cmd, args); err != nil {
				return err
			}
			if _, err := qbt.ParseFilePriority(args[1]); err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			if _, err := parseFileIndexes(args[2:]); err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeSingleTorrentHashArgs(opts)(cmd, args, toComplete)
			}
			if len(args) == 1 {
				return completeStatic([]string{"skip", "normal", "high", "maximum", "0", "1", "6", "7"}, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			priority, err := qbt.ParseFilePriority(args[1])
			if err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			ids, err := parseFileIndexes(args[2:])
			if err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			return app.SetFilePriority(hash, ids, priority)
		}),
	}

	renameFile := newTorrentPathRenameCmd(opts, false)
	renameFolder := newTorrentPathRenameCmd(opts, true)
	cmd.AddCommand(priority, renameFile, renameFolder)
	return cmd
}

func newTorrentPathRenameCmd(opts *CLIOptions, folder bool) *cobra.Command {
	name, short, aliases := "rename-file", "Rename a torrent file", []string{"rename"}
	if folder {
		name, short, aliases = "rename-folder", "Rename a torrent folder", []string{"folder"}
	}
	return &cobra.Command{
		Use:     name + " <hash> <old-path> <new-path>",
		Aliases: aliases,
		Short:   short,
		Args:    cobra.ExactArgs(3),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			if folder {
				return app.RenameTorrentFolder(hash, args[1], args[2])
			}
			return app.RenameTorrentFile(hash, args[1], args[2])
		}),
	}
}

func parseFileIndexes(values []string) ([]int, error) {
	ids := make([]int, len(values))
	seen := make(map[int]struct{}, len(values))
	for i, value := range values {
		id, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || id < 0 {
			return nil, fmt.Errorf("invalid file index %q", value)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate file index %d", id)
		}
		seen[id] = struct{}{}
		ids[i] = id
	}
	return ids, nil
}

func renderTorrentFiles(w io.Writer, app *App, files []qbt.TorrentFile) {
	tw := tabwriter.NewWriter(tableOutput(w), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "INDEX\tPRIORITY\tPROGRESS\tSIZE\tAVAILABILITY\tNAME")
	for _, file := range files {
		priority := qbt.FilePriority(file.Priority).String()
		fmt.Fprintf(tw, "%d\t%s\t%.0f%%\t%s\t%.0f%%\t%s\n",
			file.Index, priority, file.Progress*100, app.DataUnits.FormatSize(file.Size), file.Availability*100, qbt.SanitizeServerText(file.Name))
	}
	_ = tw.Flush()
}

func newPeersCmd(opts *CLIOptions) *cobra.Command {
	jsonOutput := false
	var rid int64
	cmd := &cobra.Command{
		Use:     "peers <hash>",
		Short:   "List peers for a torrent",
		GroupID: "view",
		Args:    cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			return completeSingleTorrentHashArgs(opts)(cmd, args, toComplete)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			peers, err := app.FetchTorrentPeers(hash, rid)
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(app.Stdout).Encode(peers)
			}
			renderTorrentPeers(app.Stdout, app, peers)
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output torrent peers as JSON")
	cmd.Flags().Int64Var(&rid, "rid", 0, "Per-torrent peer response ID for an incremental update")
	return cmd
}

func renderTorrentPeers(w io.Writer, app *App, peers qbt.TorrentPeers) {
	keys := make([]string, 0, len(peers.Peers))
	for key := range peers.Peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tw := tabwriter.NewWriter(tableOutput(w), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PEER\tCLIENT\tPROGRESS\tDOWN\tUP\tCONNECTION")
	for _, key := range keys {
		peer := peers.Peers[key]
		host := qbt.SanitizeServerText(peer.IP)
		name := host
		if name == "" {
			name = qbt.SanitizeServerText(peer.I2PDestination)
		}
		if name == "" {
			name = qbt.SanitizeServerText(key)
		}
		if peer.Port > 0 && host != "" {
			name = net.JoinHostPort(host, strconv.Itoa(peer.Port))
		}
		fmt.Fprintf(tw, "%s\t%s\t%.1f%%\t%s\t%s\t%s\n",
			name, qbt.SanitizeServerText(peer.Client), peer.Progress*100, app.DataUnits.FormatRate(peer.DLSpeed), app.DataUnits.FormatRate(peer.UPSpeed), qbt.SanitizeServerText(peer.Connection))
	}
	_ = tw.Flush()
}
