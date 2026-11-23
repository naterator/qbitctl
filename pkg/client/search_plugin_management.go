package client

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// InstallSearchPlugin installs a search plugin from an HTTPS source URL.
// qBittorrent accepts local paths too, but qbitctl deliberately keeps this
// operation to HTTPS sources so a remote action cannot read an arbitrary local
// file and so users have an auditable, transport-protected source.
func (a *App) InstallSearchPlugin(source string) error {
	source, err := validSearchPluginSource(source)
	if err != nil {
		return err
	}
	return a.mutateSearchPlugins(
		"/api/v2/search/installPlugin",
		url.Values{"sources": {source}},
		"install search plugin",
	)
}

// UninstallSearchPlugin removes an installed search plugin by its canonical
// qBittorrent plugin ID.
func (a *App) UninstallSearchPlugin(name string) error {
	name, err := validSearchPluginName(name)
	if err != nil {
		return err
	}
	return a.mutateSearchPlugins(
		"/api/v2/search/uninstallPlugin",
		url.Values{"names": {name}},
		"uninstall search plugin",
	)
}

// SetSearchPluginEnabled changes the enabled state of an installed plugin.
func (a *App) SetSearchPluginEnabled(name string, enabled bool) error {
	name, err := validSearchPluginName(name)
	if err != nil {
		return err
	}
	return a.mutateSearchPlugins(
		"/api/v2/search/enablePlugin",
		url.Values{
			"names":  {name},
			"enable": {boolString(enabled)},
		},
		"change search plugin state",
	)
}

// EnableSearchPlugin enables an installed search plugin.
func (a *App) EnableSearchPlugin(name string) error {
	return a.SetSearchPluginEnabled(name, true)
}

// DisableSearchPlugin disables an installed search plugin.
func (a *App) DisableSearchPlugin(name string) error {
	return a.SetSearchPluginEnabled(name, false)
}

// UpdateSearchPlugins asks qBittorrent to update its installed plugin list.
func (a *App) UpdateSearchPlugins() error {
	return a.mutateSearchPlugins(
		"/api/v2/search/updatePlugins",
		nil,
		"update search plugins",
	)
}

// ValidateSearchPluginName validates a canonical plugin ID before an action
// is sent to qBittorrent.
func ValidateSearchPluginName(value string) error {
	_, err := validSearchPluginName(value)
	return err
}

// ValidateSearchPluginSource validates the HTTPS-only source accepted by
// InstallSearchPlugin.
func ValidateSearchPluginSource(value string) error {
	_, err := validSearchPluginSource(value)
	return err
}

func (a *App) mutateSearchPlugins(endpoint string, params url.Values, action string) error {
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to %s: %v", action, err)
	}
	return nil
}

func validSearchPluginName(value string) (string, error) {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", codedErrf(ExitBadArgs, "Invalid search plugin ID")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", codedErrf(ExitBadArgs, "Search plugin ID required")
	}
	if strings.Contains(value, "|") {
		return "", codedErrf(ExitBadArgs, "Invalid search plugin ID")
	}
	return value, nil
}

func validSearchPluginSource(value string) (string, error) {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", codedErrf(ExitBadArgs, "Search plugin source must be an HTTPS URL")
	}
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return "", codedErrf(ExitBadArgs, "Search plugin source must be an HTTPS URL")
	}
	return value, nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
