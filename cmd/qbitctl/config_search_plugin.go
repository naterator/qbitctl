package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func newDefaultSearchPluginCmd(opts *CLIOptions) *cobra.Command {
	cmd := newSettingCmd(savedSetting{
		name: "default-search-plugin", short: "Choose the default search provider for a server profile",
		get: func(path string) (string, error) {
			return qbt.ReadDefaultSearchPluginForProfile(path, opts.Profile)
		},
	})
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List provider groups and server plugins in picker order",
			Args:  cobra.NoArgs,
			RunE: authenticatedRun(opts, func(app *App, _ []string) error {
				plugins, err := app.SearchPlugins()
				if err != nil {
					return err
				}
				return writeProviderOptions(app.Stdout, qbt.SearchProviderOptions(plugins))
			}),
		},
		&cobra.Command{
			Use:   "set PROVIDER",
			Short: "Save a default search provider",
			Args: func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					return codedErr(exitBadArgs, "missing required parameter PROVIDER (see qbitctl config default-search-plugin list)")
				}
				if err := cobra.ExactArgs(1)(cmd, args); err != nil {
					return err
				}
				if opts.URL != "" || opts.User != "" {
					return fmt.Errorf("select the profile to change with --profile")
				}
				return validateDefaultProvider(args[0])
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				value := strings.ToLower(strings.TrimSpace(args[0]))
				if value == qbt.DefaultSearchPlugins || value == "all" {
					path, err := configfile.ResolvePath(opts.ConfigPath)
					if err != nil {
						return result(cmd, err)
					}
					return result(cmd, qbt.SetDefaultSearchPluginForProfile(path, opts.Profile, value))
				}
				return authenticatedRun(opts, func(app *App, args []string) error {
					plugins, err := app.SearchPlugins()
					if err != nil {
						return err
					}
					return saveDefaultProvider(app, args[0], plugins)
				})(cmd, args)
			},
		},
	)
	return cmd
}

func validateDefaultProvider(selection string) error {
	value := strings.ToLower(strings.TrimSpace(selection))
	if value == "" || value == "multi" || strings.ContainsAny(value, "|\r\n\t") {
		return fmt.Errorf("choose one provider ID, enabled, or all")
	}
	return nil
}

func saveDefaultProvider(app *App, selection string, plugins []qbt.SearchPlugin) error {
	if err := validateDefaultProvider(selection); err != nil {
		return err
	}
	id, err := qbt.ResolveSearchPluginSelection(selection, plugins)
	if err != nil {
		return err
	}
	if id != qbt.ValidateDefaultSearchPlugin(id, qbt.NormalizeSearchPlugins(plugins)) {
		return fmt.Errorf("search plugin %q is disabled", qbt.SanitizeServerText(id))
	}
	return qbt.SetDefaultSearchPluginForProfile(app.ConfigPath(), app.ProfileName(), id)
}

func writeProviderOptions(out io.Writer, options []qbt.SearchProviderOption) error {
	var style cliStyle
	if terminal, ok := out.(*styledCLIOutput); ok {
		style = terminal.style
	}
	rows := make([][]string, 0, len(options))
	for _, option := range options {
		status := "disabled"
		if option.Group {
			status = "group"
		} else if option.Enabled {
			status = "enabled"
		}
		rows = append(rows, []string{qbt.SanitizeServerText(option.ID), qbt.SanitizeServerText(option.Label), status})
	}
	return writeTable(out, style, []string{"ID", "NAME", "STATUS"}, rows)
}
