package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

type settingChoice struct {
	value string
	label string
}

type savedSetting struct {
	name    string
	short   string
	arg     string
	get     func(path string) (string, error)
	set     func(path, value string) error
	choices []settingChoice
	list    func(*cobra.Command) error
}

func newConfigCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"c", "co"},
		Short:   "Inspect and change saved qbitctl preferences",
		Long:    "Inspect and change saved qbitctl preferences. Use profile for server connections and credentials.",
		GroupID: "setup",
		Args:    cobra.NoArgs,
		RunE:    showCommandHelp,
	}
	legacyWrite := newProfileWriteCmd(opts)
	legacyWrite.Hidden = true

	cmd.AddCommand(
		newSettingCmd(tuiDefaultSetting()),
		newSettingCmd(colorSchemeSetting()),
		newSettingCmd(dataUnitsSetting()),
		newSettingCmd(refreshIntervalSetting()),
		newSettingCmd(httpTimeoutSetting()),
		newDefaultSearchPluginCmd(opts),
		newSettingCmd(savedSetting{name: "config-file", short: "Show the active config file path", get: filepath.Abs}),
		newSettingCmd(savedSetting{name: "search-cache", short: "Show the search cache path", get: func(string) (string, error) {
			return qbt.SearchResultsCachePath()
		}}),
		legacyWrite,
	)
	for _, setting := range displaySettings() {
		cmd.AddCommand(newSettingCmd(setting))
	}
	return cmd
}

func showCommandHelp(cmd *cobra.Command, _ []string) error { return cmd.Help() }

func newSettingCmd(setting savedSetting) *cobra.Command {
	cmd := &cobra.Command{Use: setting.name, Short: setting.short, Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Print the current value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := configfile.ResolvePath(commandConfigPath(cmd))
			if err != nil {
				return result(cmd, err)
			}
			value, err := setting.get(path)
			if err != nil {
				return result(cmd, err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
			return err
		},
	})
	if setting.set != nil {
		set := &cobra.Command{
			Use:   "set " + setting.arg,
			Short: "Save a new value",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				path, err := configfile.ResolvePath(commandConfigPath(cmd))
				if err != nil {
					return result(cmd, err)
				}
				return result(cmd, setting.set(path, args[0]))
			},
		}
		for _, choice := range setting.choices {
			set.ValidArgs = append(set.ValidArgs, choice.value)
		}
		cmd.AddCommand(set)
	}
	if len(setting.choices) > 0 {
		cmd.AddCommand(&cobra.Command{
			Use:   "list",
			Short: "List available values",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if setting.list != nil {
					return setting.list(cmd)
				}
				for _, choice := range setting.choices {
					line := choice.value
					if choice.label != "" {
						line += "\t" + choice.label
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
						return err
					}
				}
				return nil
			},
		})
	}
	return cmd
}

func tuiDefaultSetting() savedSetting {
	return savedSetting{
		name: "tui-default", short: "Choose whether bare qbitctl starts the TUI", arg: "true|false",
		get: func(path string) (string, error) {
			value, err := qbt.ReadTUIDefault(path)
			return strconv.FormatBool(value), err
		},
		set: func(path, value string) error {
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("tui-default requires true or false")
			}
			return qbt.SetTUIDefault(path, enabled)
		},
		choices: []settingChoice{{value: "true"}, {value: "false"}},
	}
}

func colorSchemeSetting() savedSetting {
	themes := terminaltheme.All()
	choices := make([]settingChoice, 0, len(themes))
	for _, theme := range themes {
		choices = append(choices, settingChoice{value: string(theme.ID), label: theme.Label})
	}
	return savedSetting{
		name: "color-scheme", short: "Choose the shared CLI and TUI color scheme", arg: "SCHEME",
		get: func(path string) (string, error) {
			id, _ := terminaltheme.Load(path)
			return string(id), nil
		},
		set: func(path, value string) error {
			for _, theme := range themes {
				if strings.EqualFold(value, string(theme.ID)) || strings.EqualFold(value, theme.Label) {
					return terminaltheme.Save(path, theme.ID)
				}
			}
			return fmt.Errorf("unknown color scheme %q (see qbitctl config color-scheme list)", value)
		},
		choices: choices,
		list: func(cmd *cobra.Command) error {
			out := cmd.OutOrStdout()
			style := newCLIStyle(out, commandConfigPath(cmd), true)
			rows := make([][]string, 0, len(themes))
			for _, theme := range themes {
				rows = append(rows, []string{string(theme.ID), theme.Label, paletteSwatches(theme, style.profile)})
			}
			return writeTable(out, style, []string{"NAME", "TITLE", "SWATCHES"}, rows)
		},
	}
}

func dataUnitsSetting() savedSetting {
	return savedSetting{
		name: "data-units", short: "Choose shared CLI and TUI transfer units", arg: "human|bytes",
		get: func(path string) (string, error) {
			units, err := qbt.ReadDataUnits(path)
			return string(units), err
		},
		set:     func(path, value string) error { return qbt.SetDataUnits(path, qbt.DataUnits(value)) },
		choices: []settingChoice{{value: "human"}, {value: "bytes"}},
	}
}

func refreshIntervalSetting() savedSetting {
	return savedSetting{
		name: "refresh-interval", short: "Set the TUI automatic refresh interval", arg: "adaptive|DURATION",
		get: func(path string) (string, error) {
			interval, err := configfile.ReadRefreshInterval(path)
			return durationPreference(interval, configfile.AdaptiveRefresh), err
		},
		set: func(path, value string) error {
			interval, err := configfile.ParseRefreshInterval(value)
			if err != nil {
				return err
			}
			return configfile.SaveRefreshInterval(path, interval)
		},
		choices: []settingChoice{{"adaptive", "5s while active, 15s while idle"}, {"5s", "Example fixed interval (minimum 1s)"}},
	}
}

func httpTimeoutSetting() savedSetting {
	return savedSetting{
		name: "http-timeout", short: "Set the qBittorrent HTTP request timeout override", arg: "default|DURATION",
		get: func(path string) (string, error) {
			timeout, err := qbt.ReadHTTPTimeout(path)
			return durationPreference(timeout, "default"), err
		},
		set: func(path, value string) error {
			var timeout time.Duration
			if value != "default" {
				var err error
				timeout, err = time.ParseDuration(value)
				if err != nil {
					return err
				}
			}
			return qbt.SetHTTPTimeout(path, timeout)
		},
	}
}

func durationPreference(value time.Duration, fallback string) string {
	if value == 0 {
		return fallback
	}
	return value.String()
}
