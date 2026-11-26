package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/naterator/qbitctl/internal/searchresults"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func resolveSearchPluginsForCommand(cmd *cobra.Command, app *App, requested string) (string, error) {
	if cmd.Flags().Changed("plugin") {
		return app.ResolveSearchPlugins(requested)
	}

	selection := qbt.DefaultSearchPlugins
	if saved, err := qbt.ReadDefaultSearchPlugin(app.ConfigPath()); err == nil && strings.TrimSpace(saved) != "" {
		selection = saved
	}
	if isDefaultSearchPluginGroup(selection) {
		return qbt.ValidateDefaultSearchPlugin(selection, nil), nil
	}

	plugins, err := app.SearchPlugins()
	if err != nil {
		return qbt.DefaultSearchPlugins, nil
	}
	return qbt.ValidateDefaultSearchPlugin(selection, plugins), nil
}

func isDefaultSearchPluginGroup(selection string) bool {
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case qbt.DefaultSearchPlugins, "all":
		return true
	default:
		return false
	}
}

func renderSearchPlugins(w io.Writer, plugins []qbt.SearchPlugin, asJSON bool) error {
	if asJSON {
		if plugins == nil {
			plugins = []qbt.SearchPlugin{}
		}
		return json.NewEncoder(w).Encode(plugins)
	}

	if _, err := fmt.Fprintln(w, tableHeading(w, "Name\tID\tEnabled")); err != nil {
		return err
	}
	for _, plugin := range plugins {
		name := plugin.FullName
		if strings.TrimSpace(name) == "" {
			name = plugin.Name
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%t\n",
			sanitizeSearchText(name),
			sanitizeSearchText(plugin.Name),
			plugin.Enabled,
		); err != nil {
			return err
		}
	}
	return nil
}

type searchCommandOptions struct {
	sort   searchresults.Sort
	filter searchresults.Filter
	limit  int
}

func parseDataUnits(value string) (qbt.DataUnits, error) {
	if strings.TrimSpace(value) == "" {
		return qbt.DataUnits(""), nil
	}
	parsed, err := qbt.ParseDataUnits(value)
	if err != nil {
		return qbt.DataUnits(""), codedErr(exitBadArgs, err.Error())
	}
	return parsed, nil
}

func parseSearchCommandOptions(sortValue string, minSeeders int64, minSeedersSet bool, minSize string, minSizeSet bool, maxSize string, maxSizeSet bool, filterValue string, limit int) (searchCommandOptions, error) {
	sort, err := searchresults.ParseSort(sortValue)
	if err != nil {
		return searchCommandOptions{}, codedErr(exitBadArgs, err.Error())
	}
	if limit < 0 {
		return searchCommandOptions{}, codedErr(exitBadArgs, "--limit must be nonnegative")
	}

	filter := searchresults.Filter{Query: filterValue}
	if minSeedersSet {
		filter.MinSeeders = &minSeeders
	}
	if minSizeSet {
		value, parseErr := searchresults.ParseSize(minSize)
		if parseErr != nil {
			return searchCommandOptions{}, codedErr(exitBadArgs, fmt.Sprintf("--min-size: %v", parseErr))
		}
		filter.MinSize = &value
	}
	if maxSizeSet {
		value, parseErr := searchresults.ParseSize(maxSize)
		if parseErr != nil {
			return searchCommandOptions{}, codedErr(exitBadArgs, fmt.Sprintf("--max-size: %v", parseErr))
		}
		filter.MaxSize = &value
	}
	if err := filter.Validate(); err != nil {
		return searchCommandOptions{}, codedErr(exitBadArgs, err.Error())
	}
	return searchCommandOptions{sort: sort, filter: filter, limit: limit}, nil
}

func renderSearchTable(w io.Writer, results []qbt.SearchResult, tty bool, width int, units qbt.DataUnits) error {
	unitHeader := "Size"
	if strings.EqualFold(string(units), "bytes") {
		unitHeader = "Size (bytes)"
	}
	if !tty {
		if _, err := fmt.Fprintf(w, "ID\tName\t%s\tSeeders\tLeechers\tSource\n", unitHeader); err != nil {
			return err
		}
		for _, item := range results {
			id := "-"
			if item.ID != 0 {
				id = fmt.Sprintf("%d", item.ID)
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
				id,
				sanitizeSearchText(item.Name),
				units.FormatSize(item.Size),
				item.Seeders,
				item.Leechers,
				sanitizeSearchText(item.Source),
			); err != nil {
				return err
			}
		}
		return nil
	}

	if width <= 0 {
		width = 80
	}
	// Keep the title column bounded so long plugin titles don't push the
	// remaining search fields past the terminal edge.
	nameWidth := width - 54
	if nameWidth < 8 {
		nameWidth = 8
	}
	table := tabwriter.NewWriter(tableOutput(w), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(table, "ID\tName\t%s\tSeeders\tLeechers\tSource\n", unitHeader); err != nil {
		return err
	}
	for _, item := range results {
		id := "-"
		if item.ID != 0 {
			id = fmt.Sprintf("%d", item.ID)
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%d\t%s\n",
			id,
			truncateSearchName(sanitizeSearchText(item.Name), nameWidth),
			units.FormatSize(item.Size),
			item.Seeders,
			item.Leechers,
			sanitizeSearchText(item.Source),
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func truncateSearchName(value string, width int) string {
	if width <= 0 {
		return value
	}
	return ansi.Truncate(value, width, "…")
}

func searchTerminalWidth(w io.Writer) int {
	file, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(file.Fd())
	if err != nil {
		return 0
	}
	return width
}
