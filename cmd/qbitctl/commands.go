package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	qbt "github.com/naterator/qbitctl/pkg/client"

	"github.com/spf13/cobra"
)

func newConfigCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"c", "co"},
		Short:   "Manage qBittorrent credentials",
		GroupID: "setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			return result(resultOK(qbt.InteractiveConfig(os.Stdin, os.Stdout, makeHiddenInput(os.Stdin))))
		},
	}
	writeOutput := ""
	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write a config file non-interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeConfigWrite(*opts, writeOutput)
		},
	}
	writeCmd.Flags().StringVarP(&writeOutput, "output", "o", "", "Path to write config.json")
	cmd.AddCommand(writeCmd)
	return cmd
}

func resultOK(rc int) error {
	if rc == exitOK {
		return nil
	}
	return &qbt.CodedError{Code: rc, Message: "operation failed"}
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
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return result(codedErr(exitBadArgs, "invalid completion shell"))
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
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("qbitctl %s\n", qbitctlVersion)
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
				fmt.Fprintf(cmd.ErrOrStderr(), "[ERROR] %v\n", err)
				return result(codedErr(exitActionFail, "update failed"))
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
		Args:    cobra.NoArgs,
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			switch {
			case serverJSON:
				return result(app.ShowServerInfoJSON())
			case asJSON:
				return result(app.ShowServerInfoAsJSON())
			}
			return result(app.ShowServerInfo())
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	return cmd
}

func newAddCmd(opts *CLIOptions) *cobra.Command {
	var searchID int64
	cmd := &cobra.Command{
		Use:     "add <torrent-file|magnet-link> | --search-id <id>",
		Aliases: []string{"a", "ad"},
		Short:   "Add a torrent file, magnet link, or cached search result",
		GroupID: "change",
		Args: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("search-id") {
				return cobra.ExactArgs(1)(cmd, args)
			}
			if searchID <= 0 || len(args) != 0 {
				return codedErr(exitBadArgs, "Provide a positive --search-id without a torrent file or link")
			}
			return nil
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			if searchID > 0 {
				return result(app.AddSearchResult(searchID))
			}
			return result(app.AddTorrent(args[0]))
		}),
	}
	cmd.Flags().Int64Var(&searchID, "search-id", 0, "Add this ID from the latest search")
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
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			switch {
			case serverJSON:
				return result(app.ShowAllTorrentsInfoJSON())
			case asJSON:
				return result(app.ShowAllTorrentsInfoAsJSON())
			case tmpl != "":
				return result(app.ShowAllTorrentsTemplate(tmpl))
			case fieldsValue != "":
				fields, err := qbt.ParseSelectableTorrentFields(fieldsValue)
				if err != nil {
					return result(codedErr(exitBadArgs, err.Error()))
				}
				return result(app.ShowAllTorrentsFields(fields, !noHeader))
			}
			return result(app.ShowAllTorrentsInfo())
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Render output with a Go template")
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows")
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "Omit the header row when using --fields")
	return cmd
}

func newSearchCmd(opts *CLIOptions) *cobra.Command {
	plugin := "jackett"
	searchTimeout := 30 * time.Second
	jsonOutput := false
	cmd := &cobra.Command{
		Use:     "search <pattern>",
		Short:   "Search torrents with a qBittorrent search plugin",
		GroupID: "view",
		Args: func(cmd *cobra.Command, args []string) error {
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
			return nil
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			results, err := app.Search(strings.Join(args, " "), plugin, searchTimeout)
			if err != nil {
				return result(err)
			}
			if err := app.CacheSearchResults(results); err != nil {
				fmt.Fprintf(app.Stderr, "[WARN] %v\n", err)
			}
			if jsonOutput {
				if err := json.NewEncoder(app.Stdout).Encode(results); err != nil {
					return result(codedErr(exitFetchFail, fmt.Sprintf("failed to render search results: %v", err)))
				}
				return nil
			}
			fmt.Fprintln(app.Stdout, "ID\tName\tSize (bytes)\tSeeders\tLeechers\tSource")
			for _, item := range results {
				id := "-"
				if item.ID != 0 {
					id = strconv.FormatInt(item.ID, 10)
				}
				fmt.Fprintf(app.Stdout, "%s\t%s\t%d\t%d\t%d\t%s\n",
					id,
					sanitizeSearchText(item.Name),
					item.Size,
					item.Seeders,
					item.Leechers,
					sanitizeSearchText(item.Source),
				)
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&plugin, "plugin", plugin, "Search plugin to use")
	cmd.Flags().DurationVar(&searchTimeout, "timeout", searchTimeout, "Time to wait before returning available results, for example 30s or 2m")
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output search results as JSON")
	return cmd
}

func sanitizeSearchText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
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
		Args:    cobra.ExactArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			applyPositionalHash(opts, args[0])
			hash, err := app.ResolveHash(opts.Hash)
			if err != nil {
				return result(err)
			}
			switch {
			case serverJSON:
				return result(app.ShowSingleTorrentInfoJSON(hash))
			case asJSON:
				return result(app.ShowSingleTorrentInfoAsJSON(hash))
			case tmpl != "":
				return result(app.ShowSingleTorrentTemplate(hash, tmpl))
			case fieldsValue != "":
				fields, err := qbt.ParseSelectableTorrentFields(fieldsValue)
				if err != nil {
					return result(codedErr(exitBadArgs, err.Error()))
				}
				return result(app.ShowSingleTorrentFields(hash, fields, !noHeader))
			}
			return result(app.ShowSingleTorrentInfo(hash))
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Render output with a Go template")
	cmd.Flags().StringVar(&fieldsValue, "fields", "", "Render selected fields as tab-separated rows")
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "Omit the header row when using --fields")
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
		GroupID:   "view",
		Args:      cobra.ExactArgs(2),
		ValidArgs: getFieldNames,
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			applyPositionalHash(opts, args[1])
			hash, err := app.ResolveHash(opts.Hash)
			if err != nil {
				return result(err)
			}
			switch {
			case serverJSON:
				return result(app.ShowSingleTorrentInfoJSON(hash))
			case asJSON:
				return result(app.GetFieldJSON(hash, qbt.CanonicalGetField(args[0])))
			}
			return result(app.GetField(hash, qbt.CanonicalGetField(args[0])))
		}),
	}
	cmd.Flags().BoolVarP(&serverJSON, "server-json", "J", false, "Output the raw qBittorrent JSON response")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output as JSON")
	return cmd
}

func newSetCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:       "set <field> <value> <hash>",
		Aliases:   []string{"se"},
		Short:     "Update one torrent property",
		GroupID:   "change",
		Args:      cobra.ExactArgs(3),
		ValidArgs: setFieldNames,
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			applyPositionalHash(opts, args[2])
			hash, err := app.ResolveHash(opts.Hash)
			if err != nil {
				return result(err)
			}
			return result(app.SetField(hash, qbt.CanonicalSetField(args[0]), args[1]))
		}),
	}
}

func newHashCmd(opts *CLIOptions, use, short string, aliases []string, fn func(*App, string) error) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
		GroupID: "control",
		Args:    cobra.MinimumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashInputs := collectHashInputs(opts, args)
			hashes, err := app.ResolveHashes(hashInputs)
			if err != nil {
				return result(err)
			}
			return result(fn(app, strings.Join(hashes, "|")))
		}),
	}
}

func newMoveCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "move <path> <hash>",
		Aliases: []string{"m", "mo"},
		Short:   "Move torrent data on the server",
		GroupID: "change",
		Args:    cobra.ExactArgs(2),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			applyPositionalHash(opts, args[1])
			hash, err := app.ResolveHash(opts.Hash)
			if err != nil {
				return result(err)
			}
			return result(app.MoveTorrent(hash, args[0]))
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
		Use:     use,
		Aliases: aliases,
		Short:   short,
		GroupID: "change",
		Args:    cobra.MinimumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashInputs := collectHashInputs(opts, args)
			hashes, err := app.ResolveHashes(hashInputs)
			if err != nil {
				return result(err)
			}
			return result(app.StopAndRemoveTorrent(strings.Join(hashes, "|"), deleteFiles))
		}),
	}
}
