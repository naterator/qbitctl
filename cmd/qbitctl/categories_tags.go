package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// newCategoriesCmd creates the category catalog command. The root command
// registers it; keeping construction separate makes the command useful to
// tests and lets the root keep ownership of its complete command list.
//
// The bare form remains a list operation for scripting convenience:
//
//	qbitctl categories [name]
//
// Explicit get/list, create, edit, and remove subcommands are available when
// the operation should be obvious in a batch file or shell history.
func newCategoriesCmd(opts *CLIOptions) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "categories [name]",
		Aliases: []string{"category", "cat"},
		Short:   "List or manage qBittorrent categories",
		GroupID: "change",
		Args:    cobra.MaximumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return runCategoryGet(app, args, jsonOutput)
		}),
	}
	cmd.PersistentFlags().BoolVarP(&jsonOutput, "json", "j", false, "Output categories as JSON")

	get := &cobra.Command{
		Use:     "get [name]",
		Aliases: []string{"list"},
		Short:   "List categories or show one category",
		Args:    cobra.MaximumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return runCategoryGet(app, args, jsonOutput)
		}),
	}

	var create *cobra.Command
	create = &cobra.Command{
		Use:   "create <name> [save-path]",
		Short: "Create a category",
		Args: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing categories")
			}
			return cobra.RangeArgs(1, 2)(cmd, args)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			savePath := ""
			if len(args) == 2 {
				savePath = args[1]
			}
			downloadPath, enabled := categoryPathFlags(create)
			if downloadPath != nil || enabled != nil {
				return app.CreateCategoryWithPaths(args[0], savePath, downloadPath, enabled)
			}
			return app.CreateCategory(args[0], savePath)
		}),
	}
	bindCategoryPathFlags(create)

	var edit *cobra.Command
	edit = &cobra.Command{
		Use:   "edit <name> [save-path]",
		Short: "Edit a category's save and download paths",
		Args: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing categories")
			}
			return cobra.RangeArgs(1, 2)(cmd, args)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			savePath := ""
			if len(args) == 2 {
				savePath = args[1]
			}
			downloadPath, enabled := categoryPathFlags(edit)
			if downloadPath != nil || enabled != nil {
				return app.EditCategoryWithPaths(args[0], savePath, downloadPath, enabled)
			}
			return app.EditCategory(args[0], savePath)
		}),
	}
	bindCategoryPathFlags(edit)

	remove := &cobra.Command{
		Use:   "remove <name...>",
		Short: "Remove one or more categories",
		Args: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing categories")
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return app.RemoveCategories(args...)
		}),
	}

	cmd.AddCommand(get, create, edit, remove)
	return cmd
}

func bindCategoryPathFlags(cmd *cobra.Command) {
	cmd.Flags().String("download-path", "", "Incomplete/download directory on the qBittorrent host")
	cmd.Flags().Bool("download-path-enabled", false, "Enable or disable the category's download-path override")
}

func categoryPathFlags(cmd *cobra.Command) (*string, *bool) {
	path, _ := cmd.Flags().GetString("download-path")
	enabled, _ := cmd.Flags().GetBool("download-path-enabled")
	var pathValue *string
	if cmd.Flags().Changed("download-path") {
		value := path
		pathValue = &value
	}
	var enabledValue *bool
	if cmd.Flags().Changed("download-path-enabled") {
		value := enabled
		enabledValue = &value
	}
	return pathValue, enabledValue
}

func runCategoryGet(app *App, args []string, jsonOutput bool) error {
	categories, err := app.GetCategories()
	if err != nil {
		return err
	}
	if len(args) == 1 {
		name := strings.TrimSpace(args[0])
		filtered := categories[:0]
		for _, category := range categories {
			if strings.EqualFold(category.Name, name) {
				filtered = append(filtered, category)
			}
		}
		if len(filtered) == 0 {
			return codedErr(exitBadArgs, fmt.Sprintf("Category not found: %s", qbt.SanitizeServerText(name)))
		}
		categories = filtered
	}
	return renderCategories(app.Stdout, categories, jsonOutput)
}

func renderCategories(w io.Writer, categories []qbt.Category, jsonOutput bool) error {
	if jsonOutput {
		if categories == nil {
			categories = []qbt.Category{}
		}
		if err := json.NewEncoder(w).Encode(categories); err != nil {
			return codedErr(exitFetchFail, fmt.Sprintf("Failed to render categories: %v", err))
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tSAVE PATH\tDOWNLOAD PATH"); err != nil {
		return err
	}
	for _, category := range categories {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n",
			qbt.SanitizeServerText(category.Name),
			qbt.SanitizeServerText(category.SavePath),
			qbt.SanitizeServerText(categoryDownloadPathLabel(category.DownloadPath)),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func categoryDownloadPathLabel(raw json.RawMessage) string {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) || bytes.Equal(value, []byte("false")) {
		return "default"
	}
	var path string
	if json.Unmarshal(value, &path) == nil {
		if strings.TrimSpace(path) == "" {
			return "default"
		}
		return path
	}
	var enabled bool
	if json.Unmarshal(value, &enabled) == nil {
		if enabled {
			return "enabled"
		}
		return "default"
	}
	return "unknown"
}

// newTagsCmd creates both the server tag catalog operations and per-torrent
// tag assignment operations. `delete` changes the server catalog; `remove`
// only detaches tags from the selected torrents.
func newTagsCmd(opts *CLIOptions) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "tags [tag]",
		Aliases: []string{"tag"},
		Short:   "List or manage qBittorrent tags",
		GroupID: "change",
		Args:    cobra.MaximumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return runTagGet(app, args, jsonOutput)
		}),
	}
	cmd.PersistentFlags().BoolVarP(&jsonOutput, "json", "j", false, "Output tags as JSON")

	get := &cobra.Command{
		Use:     "get [tag]",
		Aliases: []string{"list"},
		Short:   "List tags or show one tag",
		Args:    cobra.MaximumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return runTagGet(app, args, jsonOutput)
		}),
	}

	create := &cobra.Command{
		Use:   "create <tag...>",
		Short: "Create server tag names",
		Args: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing tags")
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return app.CreateTags(args...)
		}),
	}

	delete := &cobra.Command{
		Use:   "delete <tag...>",
		Short: "Delete names from the server tag catalog",
		Args: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing tags")
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return app.DeleteTags(args...)
		}),
	}

	add := newTorrentTagMutationCmd(opts, "add", "Attach tags to selected torrents", true, &jsonOutput)
	remove := newTorrentTagMutationCmd(opts, "remove", "Detach tags from selected torrents", false, &jsonOutput)
	cmd.AddCommand(get, create, delete, add, remove)
	return cmd
}

func newTorrentTagMutationCmd(opts *CLIOptions, action, short string, add bool, jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:     action + " <tag[,tag...]> <hash...>",
		Aliases: map[bool][]string{true: {"attach"}, false: {"detach"}}[add],
		Short:   short,
		Args: func(cmd *cobra.Command, args []string) error {
			if *jsonOutput {
				return codedErr(exitBadArgs, "--json is only supported when listing tags")
			}
			return cobra.MinimumNArgs(2)(cmd, args)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) >= 1 {
				return completeTorrentHashes(opts, nil, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			hashes, err := resolveTagHashes(app, args[1:])
			if err != nil {
				return err
			}
			if add {
				return app.AddTorrentTags(hashes, args[0])
			}
			return app.RemoveTorrentTags(hashes, args[0])
		}),
	}
}

func resolveTagHashes(app *App, inputs []string) (string, error) {
	if len(inputs) == 1 && strings.EqualFold(strings.TrimSpace(inputs[0]), "all") {
		return "all", nil
	}
	resolved, err := app.ResolveHashes(inputs)
	if err != nil {
		return "", err
	}
	return strings.Join(resolved, "|"), nil
}

func runTagGet(app *App, args []string, jsonOutput bool) error {
	tags, err := app.GetTags()
	if err != nil {
		return err
	}
	if len(args) == 1 {
		name := strings.TrimSpace(args[0])
		filtered := tags[:0]
		for _, tag := range tags {
			if strings.EqualFold(tag, name) {
				filtered = append(filtered, tag)
			}
		}
		if len(filtered) == 0 {
			return codedErr(exitBadArgs, fmt.Sprintf("Tag not found: %s", qbt.SanitizeServerText(name)))
		}
		tags = filtered
	}
	return renderTags(app.Stdout, tags, jsonOutput)
}

func renderTags(w io.Writer, tags []string, jsonOutput bool) error {
	if jsonOutput {
		if tags == nil {
			tags = []string{}
		}
		if err := json.NewEncoder(w).Encode(tags); err != nil {
			return codedErr(exitFetchFail, fmt.Sprintf("Failed to render tags: %v", err))
		}
		return nil
	}
	if _, err := fmt.Fprintln(w, "TAG"); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := fmt.Fprintln(w, qbt.SanitizeServerText(tag)); err != nil {
			return err
		}
	}
	return nil
}
