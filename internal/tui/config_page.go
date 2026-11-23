//go:build !plan9

package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

type configSettingID uint8

const (
	configSettingServer configSettingID = iota
	configSettingUsername
	configSettingPassword
	configSettingTUIDefault
	configSettingTheme
	configSettingDataUnits
	configSettingSearchProvider
	configSettingConfigFile
	configSettingSearchCache
)

type configSetting struct {
	id    configSettingID
	group string
	label string
	value string
}

// configSettings is the selectable portion of the Config page. File rows are
// included so compact terminals can scroll to their paths, but activation is
// intentionally a no-op because they are read-only metadata.
func (m *model) configSettings() []configSetting {
	return []configSetting{
		{configSettingServer, "Connection", "Server address", m.setup.url},
		{configSettingUsername, "Connection", "Username", m.setup.user},
		{configSettingPassword, "Connection", "Password", configPasswordStatus(m.setup.hasPassword)},
		{configSettingTUIDefault, "View", "TUI by default", configToggleStatus(m.setup.tuiDefault)},
		{configSettingTheme, "View", "Color scheme", configThemeStatus(m)},
		{configSettingDataUnits, "View", "Data units", m.dataUnitsLabel()},
		{configSettingSearchProvider, "Search", "Default provider", m.searchPluginLabel(m.savedSearchPlugin)},
		{configSettingConfigFile, "Files", "Config file", m.configFileStatus()},
		{configSettingSearchCache, "Files", "Search cache", searchCacheFileStatus()},
	}
}

func (m *model) configFileStatus() string {
	path := strings.TrimSpace(m.configPath)
	if path == "" {
		path = strings.TrimSpace(m.setup.path)
	}
	return absoluteConfigPagePath(path)
}

func searchCacheFileStatus() string {
	path, err := client.SearchResultsCachePath()
	if err != nil {
		return "Unavailable"
	}
	return absoluteConfigPagePath(path)
}

func absoluteConfigPagePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "Not configured"
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

// viewConfigSettings renders the complete Config panel. width and height are
// the outer panel dimensions, matching the other viewBody renderers.
func (m *model) viewConfigSettings(width, height int) []string {
	width, height = max(1, width), max(1, height)
	settings := m.configSettings()
	selected := min(max(0, m.configSelection), max(0, len(settings)-1))

	lines := make([]string, 0, len(settings)+8)
	settingLines := make([]int, len(settings))
	lastGroup := ""
	for i, setting := range settings {
		if setting.group != lastGroup {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, viewAccentStyle.Render(setting.group))
			lastGroup = setting.group
		}
		settingLines[i] = len(lines)
		lines = append(lines, viewConfigSettingRow(setting, i == selected, width-2))
	}

	// The title consumes one row in viewPanel. Keep the selected setting in the
	// visible body at compact heights while allowing the file metadata to
	// scroll.
	visible := max(1, height-1)
	start := 0
	if len(settingLines) > 0 {
		selectedLine := settingLines[selected]
		start = max(0, selectedLine-visible+1)
		// When the final setting is selected and the viewport can contain the
		// trailing metadata, bottom-align the body so the path stays visible.
		// If it would hide the selection, the normal selection-aligned window
		// wins.
		if settings[selected].id == configSettingSearchProvider || selected == len(settings)-1 {
			bottom := max(0, len(lines)-visible)
			if bottom <= selectedLine {
				start = bottom
			}
		}
		start = min(start, max(0, len(lines)-visible))
	}
	end := min(len(lines), start+visible)
	return viewPanel("Configuration", lines[start:end], width, height)
}

func viewConfigSettingRow(setting configSetting, selected bool, width int) string {
	width = max(1, width)
	rowWidth := max(1, width-2)
	row := viewConfigRow(setting.label, setting.value, rowWidth)
	if setting.id == configSettingConfigFile || setting.id == configSettingSearchCache {
		row = viewConfigMetadataRow(setting.label, setting.value, rowWidth)
	}
	if !selected {
		return viewFit("  "+row, width)
	}
	// Strip the nested field style before applying the selected-row treatment so
	// the highlight fills the entire row in both ANSI and plain terminals.
	return viewSelectedSty.Width(width).Render(viewFit("› "+ansi.Strip(row), width))
}

func viewConfigMetadataRow(label, value string, width int) string {
	const labelWidth = 12
	label = viewSafe(strings.TrimSpace(label))
	value = strings.TrimSpace(viewSafe(value))
	if value == "" {
		value = "Not configured"
	}
	labelCell := viewMutedStyle.Background(viewFieldColor).Render(" " + viewColumn(label, labelWidth) + " ")
	valueCell := viewMutedStyle.Render(" " + viewFit(value, max(1, width-labelWidth-5)))
	line := labelCell + viewRuleStyle.Render("│") + valueCell
	return viewSurface(viewFit(line, width), width, 1, viewPanelColor, viewTextStyle.GetForeground())
}

func (m *model) moveConfigSelection(delta int) {
	count := len(m.configSettings())
	if count == 0 {
		m.configSelection = 0
		return
	}
	m.configSelection = max(0, min(m.configSelection+delta, count-1))
}

func (m *model) openConfigSetting(id configSettingID) tea.Cmd {
	if m.page != configPage {
		return nil
	}
	m.configSelection = int(id)
	return m.activateConfigSetting()
}

func (m *model) activateConfigSetting() tea.Cmd {
	settings := m.configSettings()
	if len(settings) == 0 {
		return nil
	}
	index := max(0, min(m.configSelection, len(settings)-1))
	switch settings[index].id {
	case configSettingServer, configSettingUsername, configSettingPassword:
		m.openConnectionEditor(false)
		if m.dialog == nil {
			return nil
		}
		return m.dialog.focusField(int(settings[index].id - configSettingServer))
	case configSettingTUIDefault:
		return m.toggleTUIDefault()
	case configSettingTheme:
		return m.openThemePicker()
	case configSettingDataUnits:
		return m.toggleDataUnits()
	case configSettingSearchProvider:
		return m.openSearchPluginPicker()
	case configSettingConfigFile, configSettingSearchCache:
		return nil
	default:
		return nil
	}
}
