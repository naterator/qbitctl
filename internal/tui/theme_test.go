//go:build !plan9

package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestThemeCatalogHasStableDefaultAndSixteenAlternates(t *testing.T) {
	themes := allThemes()
	if got, want := len(themes), 17; got != want {
		t.Fatalf("theme count = %d, want %d", got, want)
	}
	seen := make(map[themeID]bool, len(themes))
	for _, theme := range themes {
		if theme.id == "" || seen[theme.id] {
			t.Fatalf("theme IDs are not unique: %q", theme.id)
		}
		seen[theme.id] = true
	}
	if themes[0].id != defaultThemeID || themes[0].label != "Default" {
		t.Fatalf("first theme = %#v, want Default", themes[0])
	}
	for _, id := range []themeID{"ocean", "forest", "amber", "rose", "violet", "mono", "nord", "dracula", "solarized", "gruvbox", "catppuccin", "high-contrast", "paper", "midnight", "terminal", "sunset"} {
		if !knownTheme(id) {
			t.Errorf("theme %q is missing", id)
		}
	}
}

func TestApplyThemeKeepsDefaultOutputUnchanged(t *testing.T) {
	content := viewSurface(viewTitleStyle.Render("Theme preview"), 28, 2, viewCanvasColor, viewTextStyle.GetForeground())
	if got := applyTheme(content, 28, 2, defaultThemeID); got != content {
		t.Fatalf("default theme changed rendered content")
	}
	if got := applyTheme(content, 28, 2, "ocean"); got == content || !strings.Contains(got, "Theme preview") {
		t.Fatalf("alternate theme did not remap a rendered cell: %q", got)
	}
}

func TestSaveThemePreservesOtherConfigSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"url":"http://qbittorrent:8080","username":"nate","ui":{"queue":{"sort":"progress-desc"},"search":{"sort":"seeders-desc"}},"unknown":{"keep":true}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveThemePreference(path, "ocean"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var ui map[string]json.RawMessage
	if err := json.Unmarshal(root["ui"], &ui); err != nil {
		t.Fatal(err)
	}
	var theme string
	if err := json.Unmarshal(ui["theme"], &theme); err != nil || theme != "ocean" {
		t.Fatalf("saved theme = %q (err %v), want ocean", theme, err)
	}
	if got := configStringField(t, data, "ui", "queue", "sort"); got != "progress-desc" {
		t.Fatalf("queue sort changed during theme save: %q", got)
	}
	if !configHasField(t, data, "unknown", "keep") {
		t.Fatal("unknown config section was lost during theme save")
	}
}

func TestUnknownThemeFallsBackToDefault(t *testing.T) {
	if got, err := decodeTheme(json.RawMessage(`"not-a-theme"`)); err != nil || got != defaultThemeID {
		t.Fatalf("unknown theme = %q (err %v), want Default", got, err)
	}
	if got, err := decodeTheme(json.RawMessage(`{"id":"ocean"}`)); err == nil || got != defaultThemeID {
		t.Fatalf("object theme = %q (err %v), want rejected and Default", got, err)
	}
}

func TestThemePickerPreviewCommitAndCancel(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.configPath = filepath.Join(t.TempDir(), "config.json")
	if cmd := m.openThemePicker(); cmd != nil {
		t.Fatal("opening theme picker returned an unexpected command")
	}
	if m.dialog == nil || m.dialog.kind != dialogTheme {
		t.Fatalf("theme picker dialog = %#v", m.dialog)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.theme != defaultThemeID || m.themePreview == defaultThemeID {
		t.Fatalf("moving in picker changed saved=%q preview=%q", m.theme, m.themePreview)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil || m.theme != defaultThemeID || m.themePreview != "" {
		t.Fatalf("cancel left picker state: dialog=%v saved=%q preview=%q", m.dialog != nil, m.theme, m.themePreview)
	}
	m.openThemePicker()
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.theme != "ocean" || m.themePreview != "" {
		t.Fatalf("commit saved=%q preview=%q, want ocean/no preview", m.theme, m.themePreview)
	}
	if _, err := os.Stat(m.configPath); err != nil {
		t.Fatalf("theme commit did not create config: %v", err)
	}
}
