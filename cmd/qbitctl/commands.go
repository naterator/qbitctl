package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/searchresults"
	qbt "github.com/naterator/qbitctl/pkg/client"

	"github.com/spf13/cobra"
)

func newCompletionCmd(rootCmd *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Aliases:   []string{"com"},
		Short:     "Generate shell completion scripts",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return result(cmd, codedErr(exitBadArgs, "invalid completion shell"))
			}
		},
	}
}

func codedErr(code int, msg string) error {
	return &qbt.CodedError{Code: code, Message: msg}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{"v", "ve", "ver"},
		Short:   "Show the qbitctl version",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "qbitctl %s\n", qbitctlVersion)
			return err
		},
	}
}

func newUpdateCmd() *cobra.Command {
	dryRun := false
	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upd", "up", "upgrade"},
		Short:   "Download and install the latest qbitctl release",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := makeReleaseUpdater().Run(cmd.Context(), qbitctlVersion, dryRun, cmd.OutOrStdout()); err != nil {
				return result(cmd, codedErr(exitActionFail, err.Error()))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Only check for updates without installing")
	return cmd
}

func newInfoCmd(opts *CLIOptions) *cobra.Command {
	serverJSON := false
	asJSON := false
	cmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"i", "in"},
		Short:   "Show qBittorrent server info and transfer stats",
		GroupID: "view",
		Args:    authenticatedArgs(opts, jsonModeArgs(cobra.NoArgs)),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			switch {
			case serverJSON:
				return app.ShowServerInfoJSON()
			case asJSON:
				return app.ShowServerInfoAsJSON()
			}
			return app.ShowServerInfo()
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.MarkFlagsMutuallyExclusive("server-json", "json")
	return cmd
}

func newAddCmd(opts *CLIOptions) *cobra.Command {
	var searchID int64
	var addOptions qbt.AddTorrentOptions
	var (
		contentLayout, downloadPath, stopCondition             string
		sequential, firstLastPiecePriority, skipChecking       bool
		ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit string
		shareLimitsMode, shareLimitAction                      string
	)
	prepareAddOptions := func(cmd *cobra.Command) error {
		addOptions.ContentLayout = contentLayout
		addOptions.DownloadPath = downloadPath
		addOptions.StopCondition = stopCondition
		addOptions.SequentialDownload = sequential
		addOptions.SequentialDownloadSet = cmd.Flags().Changed("sequential") || cmd.Flags().Changed("sequential-download")
		addOptions.FirstLastPiecePriority = firstLastPiecePriority
		addOptions.FirstLastPiecePrioritySet = cmd.Flags().Changed("first-last-piece-priority") || cmd.Flags().Changed("first-last-piece-prio")
		addOptions.SkipChecking = skipChecking
		addOptions.SkipCheckingSet = cmd.Flags().Changed("skip-checking")
		addOptions.ShareLimitsMode = shareLimitsMode
		addOptions.ShareLimitAction = shareLimitAction

		if cmd.Flags().Changed("ratio-limit") {
			ratio, err := qbt.ParseAddRatioLimit(ratioLimit)
			if err != nil {
				return fmt.Errorf("--ratio-limit: %v", err)
			}
			addOptions.ShareRatioLimit = &ratio
		}
		if cmd.Flags().Changed("seeding-time-limit") || cmd.Flags().Changed("seedtime-limit") {
			seconds, err := qbt.ParseSeedtimeLimit(seedingTimeLimit)
			if err != nil {
				return fmt.Errorf("--seeding-time-limit: %v", err)
			}
			minutes, err := qbt.SeedtimeLimitSecondsToMinutes(seconds)
			if err != nil {
				return fmt.Errorf("--seeding-time-limit: %v", err)
			}
			addOptions.ShareSeedingTimeLimit = &minutes
		}
		if cmd.Flags().Changed("inactive-seeding-time-limit") || cmd.Flags().Changed("inactive-seedtime-limit") {
			seconds, err := qbt.ParseSeedtimeLimit(inactiveSeedingTimeLimit)
			if err != nil {
				return fmt.Errorf("--inactive-seeding-time-limit: %v", err)
			}
			minutes, err := qbt.SeedtimeLimitSecondsToMinutes(seconds)
			if err != nil {
				return fmt.Errorf("--inactive-seeding-time-limit: %v", err)
			}
			addOptions.ShareInactiveSeedingTimeLimit = &minutes
		}
		normalized, err := qbt.NormalizeAddTorrentOptions(addOptions)
		if err != nil {
			return fmt.Errorf("invalid add option: %v", err)
		}
		addOptions = normalized
		return nil
	}
	cmd := &cobra.Command{
		Use:     "add <torrent-file|magnet-link|URL> | --search-id <id>",
		Aliases: []string{"a", "ad"},
		Short:   "Add a torrent file, magnet link, URL, or cached search result",
		GroupID: "change",
		Args: authenticatedArgs(opts, func(cmd *cobra.Command, args []string) error {
			if err := prepareAddOptions(cmd); err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			if !cmd.Flags().Changed("search-id") {
				return cobra.ExactArgs(1)(cmd, args)
			}
			if searchID <= 0 || len(args) != 0 {
				return codedErr(exitBadArgs, "Provide a positive --search-id without a torrent file or link")
			}
			return nil
		}),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			if searchID > 0 {
				return app.AddSearchResultWithOptions(searchID, addOptions)
			}
			return app.AddTorrentWithOptions(args[0], addOptions)
		}),
		ValidArgsFunction: completeAddArgs,
	}
	cmd.Flags().Int64Var(&searchID, "search-id", 0, "Add this ID from the local search history")
	cmd.Flags().StringVar(&addOptions.Category, "category", "", "Assign the torrent to this category")
	cmd.Flags().StringVar(&addOptions.SavePath, "save-path", "", "Destination directory on the qBittorrent host")
	cmd.Flags().StringVar(&addOptions.Tags, "tags", "", "Assign comma-separated tags to the torrent")
	cmd.Flags().BoolVar(&addOptions.Stopped, "stopped", false, "Add the torrent in the stopped state")
	cmd.Flags().StringVar(&contentLayout, "content-layout", "", "File layout: original, subfolder, or no-subfolder")
	cmd.Flags().StringVar(&downloadPath, "download-path", "", "Separate incomplete/download directory on the qBittorrent host")
	cmd.Flags().BoolVar(&skipChecking, "skip-checking", false, "Skip hash checking and start seeding")
	cmd.Flags().StringVar(&stopCondition, "stop-condition", "", "Stop condition: none, metadata, or files")
	cmd.Flags().BoolVar(&sequential, "sequential", false, "Download pieces sequentially")
	cmd.Flags().BoolVar(&sequential, "sequential-download", false, "Alias for --sequential")
	cmd.Flags().BoolVar(&firstLastPiecePriority, "first-last-piece-priority", false, "Prioritize the first and last pieces")
	cmd.Flags().BoolVar(&firstLastPiecePriority, "first-last-piece-prio", false, "Alias for --first-last-piece-priority")
	cmd.Flags().StringVar(&ratioLimit, "ratio-limit", "", "Share ratio limit (non-negative, -1 unlimited, or -2 global)")
	cmd.Flags().StringVar(&seedingTimeLimit, "seeding-time-limit", "", "Seeding time limit: duration, -1 unlimited, or -2 global")
	cmd.Flags().StringVar(&seedingTimeLimit, "seedtime-limit", "", "Alias for --seeding-time-limit")
	cmd.Flags().StringVar(&inactiveSeedingTimeLimit, "inactive-seeding-time-limit", "", "Inactive seeding limit: duration, -1 unlimited, or -2 global")
	cmd.Flags().StringVar(&inactiveSeedingTimeLimit, "inactive-seedtime-limit", "", "Alias for --inactive-seeding-time-limit")
	cmd.Flags().StringVar(&shareLimitsMode, "share-limits-mode", "", "Share limits mode: default, match-any, or match-all")
	cmd.Flags().StringVar(&shareLimitAction, "share-limit-action", "", "Share limit action: default, stop, remove, remove-with-content, or enable-super-seeding")
	return cmd
}

func newListCmd(opts *CLIOptions) *cobra.Command {
	serverJSON := false
	asJSON := false
	tmpl := ""
	fieldsValue := ""
	noHeader := false
	state, category, tag := "", "", ""
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"l", "li", "ls"},
		Short:   "List all torrents",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			if err := validateOutputArgs(cmd, tmpl, fieldsValue, noHeader); err != nil {
				return err
			}
			filter, err := listTorrentFilter(state, category, tag)
			if err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			if serverJSON && (filter.State == "queued" || filter.State == "checking") {
				return codedErr(exitBadArgs, "--server-json cannot be combined with --state queued or --state checking")
			}
			return nil
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			filter, err := listTorrentFilter(state, category, tag)
			if err != nil {
				return codedErr(exitBadArgs, err.Error())
			}
			switch {
			case serverJSON:
				return app.ShowAllTorrentsInfoJSONWithFilter(filter)
			case asJSON:
				return app.ShowAllTorrentsInfoAsJSONWithFilter(filter)
			case tmpl != "":
				return app.ShowAllTorrentsTemplateWithFilter(tmpl, filter)
			case fieldsValue != "":
				fields, err := qbt.ParseSelectableTorrentFields(fieldsValue)
				if err != nil {
					return codedErr(exitBadArgs, err.Error())
				}
				return app.ShowAllTorrentsFieldsWithFilter(fields, !noHeader, filter)
			}
			return app.ShowAllTorrentsInfoWithFilter(filter)
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Render output with a Go template")
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows (use hash-full for full hashes)")
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "Omit the header row when using --fields")
	cmd.Flags().StringVar(&state, "state", "", "Filter by torrent state")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category")
	cmd.Flags().StringVar(&tag, "tag", "", "Filter by tag")
	cmd.MarkFlagsMutuallyExclusive("server-json", "json", "template", "fields")
	return cmd
}

func newSearchCmd(opts *CLIOptions) *cobra.Command {
	plugin := qbt.DefaultSearchPlugins
	searchTimeout := 30 * time.Second
	jsonOutput := false
	listPlugins := false
	recent := false
	sortValue := "original"
	minSeeders := int64(0)
	minSize, maxSize, filterValue := "", "", ""
	limit := 0
	var searchOptions searchCommandOptions
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:   "search <pattern> | --list-plugins",
		Short: "Search torrents or list qBittorrent search plugins",
		Long: "Search torrents or manage qBittorrent search plugins.\n\n" +
			"Plugin actions: search plugins list, install <https-url>, enable <id>, " +
			"disable <id>, uninstall <id>, or update.",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
			if searchPluginManagementInvocation(args) {
				return validateSearchPluginManagementArgs(cmd, args)
			}
			if recent {
				if len(args) != 0 {
					return codedErr(exitBadArgs, "--recent cannot be combined with a search pattern")
				}
				if listPlugins {
					return codedErr(exitBadArgs, "--recent cannot be combined with --list-plugins")
				}
				if cmd.Flags().Changed("plugin") || cmd.Flags().Changed("timeout") {
					return codedErr(exitBadArgs, "--recent cannot be combined with --plugin or --timeout")
				}
				var err error
				searchOptions, err = parseSearchCommandOptions(
					sortValue,
					minSeeders,
					cmd.Flags().Changed("min-seeders"),
					minSize,
					cmd.Flags().Changed("min-size"),
					maxSize,
					cmd.Flags().Changed("max-size"),
					filterValue,
					limit,
				)
				return err
			}
			if listPlugins {
				if len(args) != 0 {
					return codedErr(exitBadArgs, "--list-plugins cannot be combined with a search pattern")
				}
				for _, name := range []string{"plugin", "timeout", "sort", "min-seeders", "min-size", "max-size", "filter", "limit"} {
					if cmd.Flags().Changed(name) {
						return codedErr(exitBadArgs, fmt.Sprintf("--list-plugins cannot be combined with --%s", name))
					}
				}
				return nil
			}
			if len(args) == 0 {
				return codedErr(exitBadArgs, "search pattern is required")
			}
			if strings.TrimSpace(strings.Join(args, " ")) == "" {
				return codedErr(exitBadArgs, "search pattern cannot be empty")
			}
			if searchTimeout <= 0 {
				return codedErr(exitBadArgs, "search timeout must be greater than zero")
			}
			if strings.TrimSpace(plugin) == "" {
				return codedErr(exitBadArgs, "search plugin cannot be empty")
			}
			var err error
			searchOptions, err = parseSearchCommandOptions(
				sortValue,
				minSeeders,
				cmd.Flags().Changed("min-seeders"),
				minSize,
				cmd.Flags().Changed("min-size"),
				maxSize,
				cmd.Flags().Changed("max-size"),
				filterValue,
				limit,
			)
			if err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if recent {
				return result(cmd, renderRecentSearch(cmd, opts, jsonOutput, searchOptions))
			}
			return authenticatedRun(opts, func(app *App, args []string) error {
				if searchPluginManagementInvocation(args) {
					return runSearchPluginManagement(app, args, jsonOutput)
				}
				if listPlugins {
					plugins, err := app.SearchPlugins()
					if err != nil {
						return err
					}
					return renderSearchPlugins(app.Stdout, plugins, jsonOutput)
				}
				resolvedPlugins, err := resolveSearchPluginsForCommand(cmd, app, plugin)
				if err != nil {
					return err
				}
				results, err := app.Search(strings.Join(args, " "), resolvedPlugins, searchTimeout)
				if err != nil {
					return err
				}
				if err := app.CacheSearchResultsForQuery(strings.Join(args, " "), results); err != nil {
					fmt.Fprintf(app.Stderr, "[WARN] %v\n", err)
				}
				results = searchresults.Apply(results, searchOptions.filter, searchOptions.sort, searchOptions.limit)
				if jsonOutput {
					if err := json.NewEncoder(app.Stdout).Encode(results); err != nil {
						return codedErr(exitFetchFail, fmt.Sprintf("failed to render search results: %v", err))
					}
					return nil
				}
				return renderSearchTable(app.Stdout, results, tuiTerminal(app.Stdout), searchTerminalWidth(app.Stdout), app.DataUnits)
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&plugin, "plugin", plugin, "Search plugin ID or full name; omit to use the saved default; use | to combine, or enabled/all")
	cmd.Flags().BoolVar(&listPlugins, "list-plugins", false, "List available search plugins (use --json for JSON)")
	cmd.Flags().BoolVar(&recent, "recent", false, "Show the most recent cached search without contacting qBittorrent")
	cmd.Flags().DurationVar(&searchTimeout, "timeout", searchTimeout, "Time to wait before returning available results, for example 30s or 2m")
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output search results or plugin listing as JSON")
	cmd.Flags().StringVar(&sortValue, "sort", sortValue, "Sort results: original, seeders-desc, seeders-asc, size-desc, or size-asc")
	cmd.Flags().Int64Var(&minSeeders, "min-seeders", minSeeders, "Only include results with at least this many seeders")
	cmd.Flags().StringVar(&minSize, "min-size", minSize, "Only include results at least this size, for example 500MB or 2GiB")
	cmd.Flags().StringVar(&maxSize, "max-size", maxSize, "Only include results no larger than this size, for example 4GiB")
	cmd.Flags().StringVar(&filterValue, "filter", filterValue, "Require whitespace-separated terms in the name, source, or ID")
	cmd.Flags().IntVar(&limit, "limit", limit, "Limit displayed results; 0 means no limit")
	return cmd
}

func renderRecentSearch(cmd *cobra.Command, opts *CLIOptions, jsonOutput bool, searchOptions searchCommandOptions) error {
	history, err := qbt.ReadCachedSearchHistory(*opts)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return fmt.Errorf("no cached search results available")
	}
	latest := history[0]
	results := make([]qbt.SearchResult, 0, len(latest.Results))
	for _, item := range latest.Results {
		results = append(results, qbt.SearchResult{
			ID:       item.ID,
			Name:     item.Name,
			Size:     item.Size,
			Seeders:  item.Seeders,
			Leechers: item.Leechers,
			Source:   item.Source,
		})
	}
	results = searchresults.Apply(results, searchOptions.filter, searchOptions.sort, searchOptions.limit)
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
	}
	units := opts.DataUnits
	if units == "" {
		if setup, setupErr := qbt.ReadSetup(*opts); setupErr == nil {
			if configured, unitsErr := qbt.ReadDataUnits(setup.Path); unitsErr == nil {
				units = configured
			}
		}
	}
	out := cliOutput(cmd.OutOrStdout(), commandConfigPath(cmd))
	return renderSearchTable(out, results, tuiTerminal(out), searchTerminalWidth(out), units)
}

func sanitizeSearchText(value string) string {
	return qbt.SanitizeServerText(value)
}

func newShowCmd(opts *CLIOptions) *cobra.Command {
	serverJSON := false
	asJSON := false
	tmpl := ""
	fieldsValue := ""
	noHeader := false
	cmd := &cobra.Command{
		Use:     "show <hash>",
		Aliases: []string{"s", "sh"},
		Short:   "Show info for a single torrent",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			return validateOutputArgs(cmd, tmpl, fieldsValue, noHeader)
		},
		ValidArgsFunction: completeSingleTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[0])
			if err != nil {
				return err
			}
			switch {
			case serverJSON:
				return app.ShowSingleTorrentInfoJSON(hash)
			case asJSON:
				return app.ShowSingleTorrentInfoAsJSON(hash)
			case tmpl != "":
				return app.ShowSingleTorrentTemplate(hash, tmpl)
			case fieldsValue != "":
				fields, err := qbt.ParseSelectableTorrentFields(fieldsValue)
				if err != nil {
					return codedErr(exitBadArgs, err.Error())
				}
				return app.ShowSingleTorrentFields(hash, fields, !noHeader)
			}
			return app.ShowSingleTorrentInfo(hash)
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Render output with a Go template")
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows (use hash-full for full hashes)")
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "Omit the header row when using --fields")
	cmd.MarkFlagsMutuallyExclusive("server-json", "json", "template", "fields")
	return cmd
}

func newGetCmd(opts *CLIOptions) *cobra.Command {
	serverJSON := false
	asJSON := false
	cmd := &cobra.Command{
		Use:     "get <field> <hash>",
		Aliases: []string{"g"},
		Short:   "Read one torrent property",
		Long: "Read one torrent property.\n\nAvailable fields:\n" +
			wrapFields(getFieldNames, "  ", 72),
		GroupID:           "view",
		Args:              jsonModeArgs(cobra.ExactArgs(2)),
		ValidArgsFunction: completeGetArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[1])
			if err != nil {
				return err
			}
			switch {
			case serverJSON:
				return app.ShowSingleTorrentInfoJSON(hash)
			case asJSON:
				return app.GetFieldJSON(hash, qbt.CanonicalGetField(args[0]))
			}
			return app.GetField(hash, qbt.CanonicalGetField(args[0]))
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.MarkFlagsMutuallyExclusive("server-json", "json")
	cmd.Args = authenticatedArgs(opts, cobra.MatchAll(cmd.Args, validateFieldArg(0, qbt.IsGetField, qbt.CanonicalGetField, "get")))
	return cmd
}

func newSetCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "set <field> <value> <hash...>",
		Aliases: []string{"se"},
		Short:   "Update one torrent property",
		Long: "Update one torrent property.\n\nAvailable fields:\n" +
			wrapFields(setFieldNames, "  ", 72) + "\n\nInput formats:\n\n" +
			"  up-limit / dl-limit         Bytes/sec, or a k suffix for KiB\n" +
			"  ratio-limit                 Floating point value (>= 0)\n" +
			"  seedtime-limit              90m, 2h, 3d, 7d12h, or seconds\n" +
			"                              -1/unlimited, -2/global limit\n" +
			"  inactive-seedtime-limit    Same duration and sentinel values\n" +
			"  share-limits-mode          default, match-all, or match-any\n" +
			"  share-limit-action         default, stop, remove, remove-with-content,\n" +
			"                              or enable-super-seeding\n" +
			"  seqdl / autotmm / superseed true or false",
		GroupID:           "change",
		Args:              validateSetArgs,
		ValidArgsFunction: completeSetArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashes, err := app.ResolveHashes(args[2:])
			if err != nil {
				return err
			}
			return app.SetField(strings.Join(hashes, "|"), qbt.CanonicalSetField(args[0]), args[1])
		}),
	}
	return cmd
}

func newHashCmd(opts *CLIOptions, use, short string, aliases []string, fn func(*App, string) error) *cobra.Command {
	return &cobra.Command{
		Use:               use,
		Aliases:           aliases,
		Short:             short,
		GroupID:           "control",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashes, err := app.ResolveHashes(args)
			if err != nil {
				return err
			}
			return fn(app, strings.Join(hashes, "|"))
		}),
	}
}

func newMoveCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:               "move <path> <hash...>",
		Aliases:           []string{"m", "mo"},
		Short:             "Move torrent data on the server",
		GroupID:           "change",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeMoveArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashes, err := app.ResolveHashes(args[1:])
			if err != nil {
				return err
			}
			return app.MoveTorrent(strings.Join(hashes, "|"), args[0])
		}),
	}
}

func newRemoveCmd(opts *CLIOptions, deleteFiles bool) *cobra.Command {
	use := "remove <hash...>"
	short := "Remove torrents but keep data"
	aliases := []string{"r", "rm"}
	if deleteFiles {
		use = "delete <hash...>"
		short = "Remove torrents and delete data"
		aliases = []string{"d", "de"}
	}
	var yes bool
	cmd := &cobra.Command{
		Use:               use,
		Aliases:           aliases,
		Short:             short,
		GroupID:           "change",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTorrentHashArgs(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return authenticatedRun(opts, func(app *App, args []string) error {
				_, inBatch := batchAppFromContext(cmd.Context())
				if deleteFiles && !yes && !inBatch && interactiveWorkspace(cmd) {
					return confirmAndDelete(cmd, app, args)
				}
				hashes, err := app.ResolveHashes(args)
				if err != nil {
					return err
				}
				return app.StopAndRemoveTorrent(strings.Join(hashes, "|"), deleteFiles)
			})(cmd, args)
		},
	}
	if deleteFiles {
		cmd.Flags().BoolVar(&yes, "yes", false, "Delete without interactive confirmation")
	}
	return cmd
}
