//go:build !plan9

package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestConfigSettingsAreGroupedAndMaskSecrets(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	m := newModel(t.Context(), nil)
	m.setup = setupState{
		path:        "/tmp/qbitctl/config.json",
		url:         "https://qbit.example",
		user:        "admin",
		hasPassword: true,
		tuiDefault:  true,
	}
	m.configPath = m.setup.path
	m.savedSearchPlugin = client.DefaultSearchPlugins
	m.searchPlugins = []searchPluginInfo{{Name: "jackett", FullName: "Jackett", Enabled: true}}

	plain := ansi.Strip(strings.Join(m.viewConfigSettings(80, 24), "\n"))
	for _, want := range []string{
		"Connection", "Server address", "Username", "Password", "Active profile", "Legacy single-server config", "Startup profile", "CA bundle", "System trust store", "••••••••  saved (hidden)",
		"View", "TUI by default", "Color scheme", "Data units", "Human-readable", "Refresh interval", "Adaptive (5s active / 15s idle)",
		"Search", "Default provider", "Enabled search plugins", "Files", "Config file", "Search cache",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("config page omitted %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Jackett") {
		t.Fatal("sole enabled provider replaced the saved enabled-group label")
	}
	configPath, err := filepath.Abs(m.setup.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, configPath) {
		t.Fatalf("config page omitted absolute config path %q:\n%s", configPath, plain)
	}
	cachePath, err := client.SearchResultsCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if got := searchCacheFileStatus(); !filepath.IsAbs(got) || got != cachePath {
		t.Fatalf("search cache status = %q, want absolute path %q", got, cachePath)
	}
	if strings.Index(plain, "Connection") > strings.Index(plain, "View") ||
		strings.Index(plain, "View") > strings.Index(plain, "Search") {
		t.Fatalf("config groups are out of order:\n%s", plain)
	}
}

func TestConfigSelectionMovesAndActivatesSettings(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.setup = setupState{url: "https://qbit.example", user: "admin"}

	m.moveConfigSelection(99)
	if m.configSelection != len(m.configSettings())-1 {
		t.Fatalf("selection after moving down = %d, want last", m.configSelection)
	}
	m.moveConfigSelection(-99)
	if m.configSelection != 0 {
		t.Fatalf("selection after moving up = %d, want first", m.configSelection)
	}

	for index, wantFocus := range []int{0, 1, 2} {
		m.configSelection = index
		m.dialog = nil
		m.activateConfigSetting()
		if m.dialog == nil || m.dialog.kind != dialogConfig || m.dialog.focus != wantFocus {
			focus := -1
			if m.dialog != nil {
				focus = m.dialog.focus
			}
			t.Fatalf("connection selection %d opened dialog %#v with focus %d, want %d", index, m.dialog, focus, wantFocus)
		}
	}

	m.dialog = nil
	m.setup.tuiDefault = false
	m.configSelection = int(configSettingTUIDefault)
	m.activateConfigSetting()
	if !m.setup.tuiDefault {
		t.Fatal("TUI default activation did not toggle the setting")
	}

	m.configSelection = int(configSettingTheme)
	m.activateConfigSetting()
	if m.dialog == nil || m.dialog.kind != dialogTheme {
		t.Fatalf("color scheme activation opened %#v, want theme picker", m.dialog)
	}
	m.dialog = nil

	m.configSelection = int(configSettingDataUnits)
	m.dataUnits = client.DataUnitsHuman
	m.activateConfigSetting()
	if m.dataUnits != client.DataUnitsBytes {
		t.Fatalf("data units activation = %q, want bytes", m.dataUnits)
	}

	m.configSelection = int(configSettingRefreshInterval)
	m.dialog = nil
	m.activateConfigSetting()
	if m.dialog == nil || m.dialog.kind != dialogRefreshInterval {
		t.Fatalf("refresh interval activation opened %#v, want refresh interval dialog", m.dialog)
	}

	m.configSelection = int(configSettingSearchProvider)
	m.dialog = nil
	m.activateConfigSetting()
	if m.dialog == nil || m.dialog.kind != dialogSearchPlugins {
		t.Fatalf("provider activation opened %#v, want provider picker", m.dialog)
	}

	for index, setting := range m.configSettings() {
		if setting.id != configSettingConfigFile && setting.id != configSettingSearchCache {
			continue
		}
		m.configSelection = index
		m.dialog = nil
		if cmd := m.activateConfigSetting(); cmd != nil || m.dialog != nil {
			t.Fatalf("read-only file setting %d activated command=%v dialog=%#v", index, cmd != nil, m.dialog)
		}
	}
}

func TestConfigCAFieldUsesEffectivePathAndPreservesItInEditor(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.setup = setupState{
		url:    "https://qbit.example",
		user:   "admin",
		caFile: "relative-ca.pem",
	}

	wantPath, err := filepath.Abs(m.setup.caFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.caFileStatus(); got != wantPath {
		t.Fatalf("CA status = %q, want effective absolute path %q", got, wantPath)
	}

	m.openConnectionEditor(false)
	if m.dialog == nil || len(m.dialog.fields) != 4 {
		t.Fatalf("connection editor = %#v, want four fields including CA bundle", m.dialog)
	}
	if got := m.dialog.fields[3].label; got != "CA bundle" {
		t.Fatalf("last editor field label = %q, want CA bundle", got)
	}
	if got := m.dialog.value(3); got != m.setup.caFile {
		t.Fatalf("CA editor value = %q, want saved path %q", got, m.setup.caFile)
	}
}

func TestConfigSettingsScrollAndHighlightSelection(t *testing.T) {
	cacheRoot := filepath.Join(goldenConfigRoot(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	configPath := filepath.Join(goldenConfigRoot(), "config.json")
	m := newModel(t.Context(), nil)
	m.setup = setupState{path: configPath}
	// The provider is the last editable setting; file paths should appear
	// below it whenever the compact viewport can retain the selection.
	m.configSelection = int(configSettingSearchProvider)

	lines := m.viewConfigSettings(64, 8)
	if len(lines) != 8 {
		t.Fatalf("compact config panel has %d lines, want 8", len(lines))
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Default provider") {
		t.Fatalf("selected setting was scrolled out:\n%s", plain)
	}
	if !strings.Contains(plain, configPath) || !strings.Contains(plain, filepath.Join(cacheRoot, "qbitctl", "search-results.json")) {
		t.Fatalf("trailing config metadata was unnecessarily scrolled out:\n%s", plain)
	}
	if !strings.Contains(plain, "›") {
		t.Fatalf("selected config row has no visible marker:\n%s", plain)
	}
}
