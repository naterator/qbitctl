package client

import (
	"sort"
	"strings"
	"unicode"
)

// SearchProviderOption describes a provider choice as displayed in settings.
type SearchProviderOption struct {
	ID, Label string
	Enabled   bool
	Group     bool
}

func NormalizeSearchPlugins(input []SearchPlugin) []SearchPlugin {
	plugins := make([]SearchPlugin, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, plugin := range input {
		plugin.Name = strings.TrimSpace(plugin.Name)
		plugin.FullName = strings.TrimSpace(plugin.FullName)
		if plugin.Name == "" {
			continue
		}
		if _, ok := seen[plugin.Name]; ok {
			continue
		}
		seen[plugin.Name] = struct{}{}
		plugins = append(plugins, plugin)
	}
	return plugins
}

// SearchProviderOptions returns the two built-in groups, followed by enabled
// and disabled providers in display-name order within each section.
func SearchProviderOptions(catalog []SearchPlugin) []SearchProviderOption {
	options := []SearchProviderOption{
		{ID: DefaultSearchPlugins, Label: "Enabled search plugins", Enabled: true, Group: true},
		{ID: "all", Label: "All search plugins", Enabled: true, Group: true},
	}
	plugins := NormalizeSearchPlugins(catalog)
	for _, plugin := range plugins {
		label := plugin.FullName
		if label == "" {
			runes := []rune(plugin.Name)
			runes[0] = unicode.ToUpper(runes[0])
			label = string(runes)
		}
		options = append(options, SearchProviderOption{ID: plugin.Name, Label: label, Enabled: plugin.Enabled})
	}
	ordered := options[2:]
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Enabled != right.Enabled {
			return left.Enabled
		}
		leftName := strings.ToLower(SanitizeServerText(left.Label))
		rightName := strings.ToLower(SanitizeServerText(right.Label))
		if leftName == rightName {
			return strings.ToLower(left.ID) < strings.ToLower(right.ID)
		}
		return leftName < rightName
	})
	return options
}
