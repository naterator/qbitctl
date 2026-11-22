package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"
)

// DefaultSearchPlugins is qBittorrent's selector for all currently enabled
// search plugins.
const DefaultSearchPlugins = "enabled"

// SearchPlugin describes one qBittorrent search plugin.
type SearchPlugin struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Enabled  bool   `json:"enabled"`
}

// SearchPlugins returns the installed search plugin catalog from qBittorrent.
func (a *App) SearchPlugins() ([]SearchPlugin, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/search/plugins", nil)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch search plugins: %v", err)
	}

	var plugins []SearchPlugin
	if err := json.Unmarshal(body, &plugins); err != nil || plugins == nil {
		return nil, codedErrf(ExitFetchFail, "Invalid search plugins response from qBittorrent")
	}
	return plugins, nil
}

// ResolveSearchPlugins resolves plugin IDs and display names to the
// pipe-separated selector accepted by qBittorrent. The reserved selectors
// enabled, all, and multi are passed through without a catalog request.
// When an older or unavailable server cannot provide the catalog, a selector
// made only of ID-like tokens is preserved for backwards compatibility.
func (a *App) ResolveSearchPlugins(selection string) (string, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return "", codedErrf(ExitBadArgs, "Search plugin required")
	}
	if canonical, ok := reservedSearchPluginSelector(selection); ok {
		return canonical, nil
	}

	plugins, err := a.SearchPlugins()
	if err != nil {
		if fallback, ok := legacySearchPluginSelector(selection); ok && searchPluginDiscoveryUnavailable(err) {
			return fallback, nil
		}
		return "", err
	}
	return ResolveSearchPluginSelection(selection, plugins)
}

// ResolveSearchPluginSelection is the pure catalog resolver used by callers
// that already have a plugin catalog, such as the TUI.
func ResolveSearchPluginSelection(selection string, catalog []SearchPlugin) (string, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return "", codedErrf(ExitBadArgs, "Search plugin required")
	}
	if canonical, ok := reservedSearchPluginSelector(selection); ok {
		return canonical, nil
	}

	parts := strings.Split(selection, "|")
	resolved := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", codedErrf(ExitBadArgs, "Search plugin selector contains an empty item")
		}

		matches := searchPluginMatches(part, catalog)

		switch len(matches) {
		case 0:
			return "", codedErrf(ExitBadArgs, "Unknown search plugin: %q", safeSearchPluginText(part))
		case 1:
			for _, name := range matches {
				if _, ok := seen[name]; !ok {
					resolved = append(resolved, name)
					seen[name] = struct{}{}
				}
			}
		default:
			return "", codedErrf(ExitBadArgs, "Search plugin is ambiguous: %q", safeSearchPluginText(part))
		}
	}
	return strings.Join(resolved, "|"), nil
}

func searchPluginMatches(selection string, catalog []SearchPlugin) map[string]string {
	// An exact canonical ID wins over a display-name alias. This keeps an ID
	// usable even when another plugin happens to use that text as its full name.
	exact := make(map[string]string)
	for _, plugin := range catalog {
		name := strings.TrimSpace(plugin.Name)
		if name != "" && selection == name {
			exact[name] = name
		}
	}
	if len(exact) > 0 {
		return exact
	}

	aliases := make(map[string]string)
	for _, plugin := range catalog {
		name := strings.TrimSpace(plugin.Name)
		if name == "" {
			continue
		}
		if strings.EqualFold(selection, name) || (plugin.FullName != "" && strings.EqualFold(selection, strings.TrimSpace(plugin.FullName))) {
			aliases[name] = name
		}
	}
	return aliases
}

func safeSearchPluginText(value string) string {
	return SanitizeServerText(strings.TrimSpace(value))
}

func reservedSearchPluginSelector(selection string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case DefaultSearchPlugins:
		return DefaultSearchPlugins, true
	case "all":
		return "all", true
	case "multi":
		return "multi", true
	default:
		return "", false
	}
}

func legacySearchPluginSelector(selection string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(selection), "|")
	if len(parts) == 0 {
		return "", false
	}
	resolved := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return "", false
		}
		if _, reserved := reservedSearchPluginSelector(part); reserved {
			return "", false
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		resolved = append(resolved, part)
	}
	return strings.Join(resolved, "|"), true
}

func searchPluginDiscoveryUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		// Transport failures are safe to fall back from because the caller
		// already supplied a raw selector. A malformed catalog, however, is a
		// server response and must not silently bypass validation.
		return errors.Unwrap(err) != nil
	}
	switch httpErr.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}
