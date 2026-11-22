package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/naterator/qbitctl/internal/configfile"
)

const defaultSearchPluginConfigKey = "default_search_plugin"

// ReadDefaultSearchPlugin reads the saved provider preference without
// contacting qBittorrent. Missing and empty values retain the normal enabled
// provider-group default so older config files behave as before.
func ReadDefaultSearchPlugin(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("config path is empty")
	}

	object, err := configfile.Read(path)
	if err != nil {
		return "", err
	}
	value, ok := configValue(object, defaultSearchPluginConfigKey)
	if !ok {
		return DefaultSearchPlugins, nil
	}

	var selection string
	if err := json.Unmarshal(value, &selection); err != nil {
		return "", fmt.Errorf("decode config field %q: %w", defaultSearchPluginConfigKey, err)
	}
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return DefaultSearchPlugins, nil
	}
	return selection, nil
}

// SetDefaultSearchPlugin saves one provider ID or one of qBittorrent's
// provider-group selectors. Provider IDs are intentionally not checked
// against a catalog here: the selected config may be used with a different
// server, and callers validate it after connecting. Empty selection removes
// the optional key and restores the normal enabled-provider default.
func SetDefaultSearchPlugin(path, selection string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}

	selection = strings.TrimSpace(selection)
	if selection == "" {
		return configfile.Update(path, func(object configfile.Object) (bool, error) {
			return deleteConfigValue(object, defaultSearchPluginConfigKey), nil
		})
	}
	if !validDefaultSearchPluginValue(selection) {
		return errors.New("default search plugin must be one ID or the enabled/all group selector")
	}
	if canonical, ok := reservedSearchPluginSelector(selection); ok {
		selection = canonical
	}
	encoded, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("encode config field %q: %w", defaultSearchPluginConfigKey, err)
	}

	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		setConfigValue(object, defaultSearchPluginConfigKey, encoded)
		return true, nil
	})
}

// ValidateDefaultSearchPlugin validates a saved preference against the
// currently connected server. Only an installed, enabled plugin's exact
// canonical ID is returned. The enabled and all group selectors remain valid
// regardless of the catalog; stale, disabled, display-name, ambiguous,
// multi-provider, and malformed values fall back to the normal
// enabled-provider group.
func ValidateDefaultSearchPlugin(selection string, plugins []SearchPlugin) string {
	selection = strings.TrimSpace(selection)
	if !validDefaultSearchPluginValue(selection) {
		return DefaultSearchPlugins
	}
	if canonical, ok := reservedSearchPluginSelector(selection); ok {
		if canonical == DefaultSearchPlugins || canonical == "all" {
			return canonical
		}
		return DefaultSearchPlugins
	}

	for _, plugin := range plugins {
		if plugin.Name == selection && plugin.Enabled {
			return plugin.Name
		}
	}
	return DefaultSearchPlugins
}

func validDefaultSearchPluginValue(selection string) bool {
	if selection == "" || strings.Contains(selection, "|") {
		return false
	}
	if strings.IndexFunc(selection, func(r rune) bool {
		return unicode.IsControl(r)
	}) >= 0 {
		return false
	}
	if canonical, ok := reservedSearchPluginSelector(selection); ok {
		return canonical != "multi"
	}
	return true
}

func deleteConfigValue(object configfile.Object, key string) bool {
	deleted := false
	for existing := range object {
		if strings.EqualFold(existing, key) {
			delete(object, existing)
			deleted = true
		}
	}
	return deleted
}
