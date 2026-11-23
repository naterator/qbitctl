package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// newTrackersCmd keeps the historical bare `trackers <hash>` form as a list
// operation while exposing add/edit/remove as subcommands.
func newTrackersCmd(opts *CLIOptions) *cobra.Command {
	var jsonOutput bool
	var fullURL bool

	cmd := &cobra.Command{
		Use:               "trackers <hash>",
		Aliases:           []string{"tracker"},
		Short:             "List or manage a torrent's trackers",
		GroupID:           "view",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSingleTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			trackers, err := app.FetchTrackerEntries(hash)
			if err != nil {
				return err
			}
			return renderTrackerList(app.Stdout, trackers, jsonOutput, fullURL)
		}),
	}
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output tracker records as JSON (includes full URLs)")
	cmd.Flags().BoolVar(&fullURL, "full-url", false, "Show full tracker URLs instead of hostnames")
	cmd.MarkFlagsMutuallyExclusive("json", "full-url")
	cmd.AddCommand(
		newTrackerAddCmd(opts),
		newTrackerEditCmd(opts),
		newTrackerRemoveCmd(opts),
	)
	return cmd
}

func newTrackerAddCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:               "add <hash> <url...>",
		Short:             "Add tracker URLs to a torrent",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeSingleTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			return app.AddTrackers(hash, strings.Join(args[1:], "\n"))
		}),
	}
}

func newTrackerEditCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:               "edit <hash> <url> <new-url>",
		Short:             "Replace one exact tracker URL",
		Args:              cobra.ExactArgs(3),
		ValidArgsFunction: completeSingleTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			return app.EditTracker(hash, args[1], args[2])
		}),
	}
}

func newTrackerRemoveCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:               "remove <hash> <url...>",
		Short:             "Remove exact tracker URLs from a torrent",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeSingleTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			return app.RemoveTrackers(hash, strings.Join(args[1:], "|"))
		}),
	}
}

func renderTrackerList(w io.Writer, trackers []qbt.TrackerEntry, jsonOutput, fullURL bool) error {
	if jsonOutput {
		if err := json.NewEncoder(w).Encode(trackers); err != nil {
			return codedErr(exitFetchFail, fmt.Sprintf("Failed to render tracker list: %v", err))
		}
		return nil
	}
	columns := []string{"Host", "Status", "Tier", "Peers", "Seeds", "Leeches"}
	if fullURL {
		columns[0] = "URL"
	}
	fmt.Fprintln(w, strings.Join(columns, "\t"))
	for _, tracker := range trackers {
		location := qbt.TrackerHost(tracker.URL)
		if fullURL {
			location = tracker.URL
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\n",
			qbt.SanitizeServerText(location),
			qbt.SanitizeServerText(qbt.TrackerStatusLabel(tracker.Status)),
			tracker.Tier,
			tracker.NumPeers,
			tracker.NumSeeds,
			tracker.NumLeeches,
		)
	}
	return nil
}
