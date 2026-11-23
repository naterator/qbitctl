//go:build !plan9

package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

const (
	searchPluginManageInstall = "install"
	searchPluginManageUpdate  = "update"
	searchPluginManagePrefix  = "plugin:"
	searchPluginActionEnable  = "enable"
	searchPluginActionDisable = "disable"
	searchPluginActionRemove  = "uninstall"
)

type searchPluginActionService interface {
	searchPluginService
	searchPluginAction(context.Context, string, string) error
}

func (c *tuiClient) searchPluginAction(ctx context.Context, action, value string) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, nil, nil)
	switch action {
	case searchPluginManageInstall:
		return app.InstallSearchPlugin(value)
	case searchPluginManageUpdate:
		return app.UpdateSearchPlugins()
	case searchPluginActionEnable:
		return app.EnableSearchPlugin(value)
	case searchPluginActionDisable:
		// Disabling intentionally leaves a saved default provider untouched.
		// Catalog validation will temporarily use the enabled group and will
		// restore the saved ID when this provider is enabled again.
		return app.DisableSearchPlugin(value)
	case searchPluginActionRemove:
		if err := app.UninstallSearchPlugin(value); err != nil {
			return err
		}
		return app.ResetDefaultSearchPluginIfMatches(value)
	default:
		return fmt.Errorf("unknown search plugin action: %s", client.SanitizeServerText(action))
	}
}

func (m *model) openSearchPluginManagement() tea.Cmd {
	m.dialog = &dialog{
		kind:  dialogSearchPluginManage,
		title: "Manage search plugins",
	}
	m.refreshSearchPluginManagement()
	return nil
}

func (m *model) refreshSearchPluginManagement() {
	if m.dialog == nil || m.dialog.kind != dialogSearchPluginManage {
		return
	}
	selected := ""
	if m.dialog.selected >= 0 && m.dialog.selected < len(m.dialog.items) {
		selected = m.dialog.items[m.dialog.selected].action
	}
	items := []dialogItem{
		{label: "Install plugin", action: searchPluginManageInstall, description: "from an HTTPS URL"},
		{label: "Update plugin list", action: searchPluginManageUpdate, description: "ask qBittorrent to refresh providers"},
	}
	plugins := append([]searchPluginInfo(nil), m.searchPlugins...)
	// Keep the same enabled-first ordering as the provider picker.
	sort.SliceStable(plugins, func(i, j int) bool {
		if plugins[i].Enabled != plugins[j].Enabled {
			return plugins[i].Enabled
		}
		left := strings.ToLower(searchPluginFullName(plugins[i]))
		right := strings.ToLower(searchPluginFullName(plugins[j]))
		if left == right {
			return strings.ToLower(plugins[i].Name) < strings.ToLower(plugins[j].Name)
		}
		return left < right
	})
	for _, plugin := range plugins {
		name := strings.TrimSpace(plugin.Name)
		if name == "" {
			continue
		}
		state := "disabled"
		if plugin.Enabled {
			state = "enabled"
		}
		items = append(items, dialogItem{
			label:       searchPluginFullName(plugin),
			action:      searchPluginManagePrefix + name,
			description: viewSafe(name) + " · " + state,
		})
	}
	m.dialog.items = items
	m.dialog.selected = 0
	for i, item := range items {
		if item.action == selected {
			m.dialog.selected = i
			break
		}
	}
}

func (m *model) openSearchPluginActions(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	enabled := false
	display := searchPluginLabel(name)
	for _, plugin := range m.searchPlugins {
		if plugin.Name == name {
			enabled = plugin.Enabled
			display = searchPluginFullName(plugin)
			break
		}
	}
	action := searchPluginActionEnable
	label := "Enable plugin"
	if enabled {
		action = searchPluginActionDisable
		label = "Disable plugin"
	}
	m.dialog = &dialog{
		kind:       dialogSearchPluginActions,
		title:      "Manage " + display,
		pluginName: name,
		items: []dialogItem{
			{label: label, action: action},
			{label: "Uninstall plugin", action: searchPluginActionRemove, description: "remove it from qBittorrent"},
		},
	}
	return nil
}

func (m *model) openSearchPluginInstall() tea.Cmd {
	m.dialog = &dialog{
		kind:  dialogSearchPluginInstall,
		title: "Install search plugin",
		fields: []dialogField{
			newDialogField("Source", "", "https://example/plugin.py", "Only HTTPS plugin sources are accepted"),
		},
	}
	return m.dialog.focusField(0)
}

func (m *model) submitSearchPluginManagementSelection() tea.Cmd {
	if m.dialog == nil || len(m.dialog.items) == 0 {
		return nil
	}
	action := m.dialog.items[m.dialog.selected].action
	switch {
	case action == searchPluginManageInstall:
		return m.openSearchPluginInstall()
	case action == searchPluginManageUpdate:
		return m.startSearchPluginAction(searchPluginManageUpdate, "")
	case strings.HasPrefix(action, searchPluginManagePrefix):
		return m.openSearchPluginActions(strings.TrimPrefix(action, searchPluginManagePrefix))
	default:
		return nil
	}
}

func (m *model) submitSearchPluginAction() tea.Cmd {
	if m.dialog == nil || len(m.dialog.items) == 0 || m.dialog.pluginName == "" {
		return nil
	}
	action := m.dialog.items[m.dialog.selected].action
	return m.startSearchPluginAction(action, m.dialog.pluginName)
}

func (m *model) startSearchPluginAction(action, value string) tea.Cmd {
	if m.busy != "" {
		if m.dialog != nil {
			m.dialog.status = "Finish the current request first"
		}
		return nil
	}
	service, ok := m.backend.(searchPluginActionService)
	if !ok {
		if m.dialog != nil {
			m.dialog.status = "Plugin management is unavailable"
		}
		return nil
	}
	if m.searchPluginsLoading {
		// This action reloads the catalog after mutation. Discard any older
		// picker response so it cannot restore the pre-mutation catalog.
		m.cancelSearchPlugins()
		m.searchPluginGeneration++
		m.searchPluginCancel = nil
		m.searchPluginsLoading = false
	}
	if m.dialog != nil {
		m.dialog.status = "Working…"
	}
	return m.startWork(searchPluginManageWork, "Managing search plugins", func(ctx context.Context) workDone {
		err := service.searchPluginAction(ctx, action, value)
		if err != nil {
			return workDone{pluginAction: action, pluginActionName: value, err: err}
		}
		plugins, err := service.searchPlugins(ctx)
		return workDone{pluginAction: action, pluginActionName: value, plugins: plugins, err: err}
	})
}

func (m *model) submitSearchPluginInstall(source string) tea.Cmd {
	source = strings.TrimSpace(source)
	if err := client.ValidateSearchPluginSource(source); err != nil {
		if m.dialog != nil {
			m.dialog.status = "Source must be an HTTPS URL"
		}
		return nil
	}
	return m.startSearchPluginAction(searchPluginManageInstall, source)
}
