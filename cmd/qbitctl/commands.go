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

func newConfigCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "config",
		Aliases:     []string{"c", "co"},
		Short:       "Manage qBittorrent credentials",
		GroupID:     "setup",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{stdinModeAnnotation: stdinInteractive},
		RunE: func(cmd *cobra.Command, args []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			path, err := qbt.InteractiveConfigContext(cmd.Context(), in, out, makeHiddenInput(in, out), opts.ConfigPath)
			if err != nil {
				return result(cmd, err)
			}
			_, err = fmt.Fprintf(out, "Saved config to %s\n", path)
			return err
		},
	}
	writeOutput := ""
	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write a config file non-interactively",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return result(cmd, qbt.ExecuteConfigWrite(*opts, writeOutput, cmd.ErrOrStderr()))
		},
	}
	writeCmd.Flags().StringVarP(&writeOutput, "output", "o", "", "Path to write config.json")
	cmd.AddCommand(writeCmd)
	return cmd
}

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
		Args:    jsonModeArgs(cobra.NoArgs),
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
	cmd := &cobra.Command{
		Use:     "add <torrent-file|magnet-link|URL> | --search-id <id>",
		Aliases: []string{"a", "ad"},
		Short:   "Add a torrent file, magnet link, URL, or cached search result",
		GroupID: "change",
		Args: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("search-id") {
				return cobra.ExactArgs(1)(cmd, args)
			}
			if searchID <= 0 || len(args) != 0 {
				return codedErr(exitBadArgs, "Provide a positive --search-id without a torrent file or link")
			}
			if flags := cmd.Flags(); flags.Changed("category") || flags.Changed("save-path") || flags.Changed("stopped") {
				return codedErr(exitBadArgs, "Add options require a torrent file, magnet link, or URL")
			}
			return nil
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			if searchID > 0 {
				return app.AddSearchResult(searchID)
			}
			return app.AddTorrentWithOptions(args[0], addOptions)
		}),
		ValidArgsFunction: completeAddArgs,
	}
	cmd.Flags().Int64Var(&searchID, "search-id", 0, "Add this ID from the local search history")
	cmd.Flags().StringVar(&addOptions.Category, "category", "", "Assign the torrent to this category")
	cmd.Flags().StringVar(&addOptions.SavePath, "save-path", "", "Destination directory on the qBittorrent host")
	cmd.Flags().BoolVar(&addOptions.Stopped, "stopped", false, "Add the torrent in the stopped state")
	return cmd
}

func newListCmd(opts *CLIOptions) *cobra.Command {
	serverJSON := false
	asJSON := false
	tmpl := ""
	fieldsValue := ""
	noHeader := false
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"l", "li", "ls"},
		Short:   "List all torrents",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validateOutputArgs(cmd, tmpl, fieldsValue, noHeader)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			switch {
			case serverJSON:
				return app.ShowAllTorrentsInfoJSON()
			case asJSON:
				return app.ShowAllTorrentsInfoAsJSON()
			case tmpl != "":
				return app.ShowAllTorrentsTemplate(tmpl)
			case fieldsValue != "":
				fields, err := qbt.ParseSelectableTorrentFields(fieldsValue)
				if err != nil {
					return codedErr(exitBadArgs, err.Error())
				}
				return app.ShowAllTorrentsFields(fields, !noHeader)
			}
			return app.ShowAllTorrentsInfo()
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Render output with a Go template")
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows")
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "Omit the header row when using --fields")
	cmd.MarkFlagsMutuallyExclusive("server-json", "json", "template", "fields")
	return cmd
}

func newSearchCmd(opts *CLIOptions) *cobra.Command {
	plugin := qbt.DefaultSearchPlugins
	searchTimeout := 30 * time.Second
	jsonOutput := false
	listPlugins := false
	sortValue := "original"
	minSeeders := int64(0)
	minSize, maxSize, filterValue := "", "", ""
	limit := 0
	var searchOptions searchCommandOptions
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:     "search <pattern> | --list-plugins",
		Short:   "Search torrents or list qBittorrent search plugins",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
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
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
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
			if err := app.CacheSearchResults(results); err != nil {
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
		}),
	}
	cmd.Flags().StringVar(&plugin, "plugin", plugin, "Search plugin ID or full name; omit to use the saved default; use | to combine, or enabled/all")
	cmd.Flags().BoolVar(&listPlugins, "list-plugins", false, "List available search plugins (use --json for JSON)")
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
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows")
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
	cmd.Args = cobra.MatchAll(cmd.Args, validateFieldArg(0, qbt.IsGetField, qbt.CanonicalGetField, "get"))
	return cmd
}

func newSetCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "set <field> <value> <hash>",
		Aliases: []string{"se"},
		Short:   "Update one torrent property",
		Long: "Update one torrent property.\n\nAvailable fields:\n" +
			wrapFields(setFieldNames, "  ", 72) + "\n\nInput formats:\n\n" +
			"  up-limit / dl-limit         Bytes/sec, or a k suffix for KiB\n" +
			"  ratio-limit                 Floating point value (>= 0)\n" +
			"  seedtime-limit              90m, 2h, 3d, 7d12h, or seconds\n" +
			"                              -1 unlimited, -2 global limit\n" +
			"  seqdl / autotmm / superseed true or false",
		GroupID:           "change",
		Args:              validateSetArgs,
		ValidArgsFunction: completeSetArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[2])
			if err != nil {
				return err
			}
			return app.SetField(hash, qbt.CanonicalSetField(args[0]), args[1])
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
		Use:               "move <path> <hash>",
		Aliases:           []string{"m", "mo"},
		Short:             "Move torrent data on the server",
		GroupID:           "change",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeMoveArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hash, err := app.ResolveHash(args[1])
			if err != nil {
				return err
			}
			return app.MoveTorrent(hash, args[0])
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
	return &cobra.Command{
		Use:               use,
		Aliases:           aliases,
		Short:             short,
		GroupID:           "change",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTorrentHashArgs(opts),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashes, err := app.ResolveHashes(args)
			if err != nil {
				return err
			}
			return app.StopAndRemoveTorrent(strings.Join(hashes, "|"), deleteFiles)
		}),
	}
}
