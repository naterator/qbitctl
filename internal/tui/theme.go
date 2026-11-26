//go:build !plan9

package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/naterator/qbitctl/internal/terminaltheme"
)

// Render in the palette with distinct role colors so the final frame can
// remap each role even when the selected palette shares colors across roles.
const renderThemeID terminaltheme.ID = "tookie"

func (m *model) activeThemeID() terminaltheme.ID {
	if terminaltheme.Known(m.themePreview) {
		return m.themePreview
	}
	if terminaltheme.Known(m.theme) {
		return m.theme
	}
	return terminaltheme.Default
}

// themeLabel reports the saved selection, rather than a temporary preview.
// Config views therefore never claim a scheme was saved before Enter was
// pressed in the picker.
func (m *model) themeLabel() string {
	return terminaltheme.Lookup(m.theme).Label
}

func themeIndex(id terminaltheme.ID) int {
	for index, theme := range terminaltheme.All() {
		if theme.ID == id {
			return index
		}
	}
	return 0
}

// openThemePicker opens the same compact, keyboard-first list used by the
// existing sort/edit selectors. Arrow movement updates themePreview so the
// entire TUI previews the highlighted palette; Enter persists the stable ID.
func (m *model) openThemePicker() tea.Cmd {
	themes := terminaltheme.All()
	items := make([]dialogItem, 0, len(themes))
	for _, theme := range themes {
		items = append(items, dialogItem{
			label:       theme.Label,
			action:      string(theme.ID),
			description: theme.Description,
		})
	}
	m.themePreview = m.theme
	m.dialog = &dialog{
		kind:     dialogTheme,
		title:    "Color scheme",
		items:    items,
		selected: themeIndex(m.theme),
	}
	return nil
}

func (m *model) previewThemeFromDialog() {
	if m.dialog == nil || m.dialog.kind != dialogTheme || len(m.dialog.items) == 0 {
		return
	}
	index := min(max(0, m.dialog.selected), len(m.dialog.items)-1)
	id := terminaltheme.ID(m.dialog.items[index].action)
	if terminaltheme.Known(id) {
		m.themePreview = id
	}
}

func (m *model) persistTheme(id terminaltheme.ID) error {
	if strings.TrimSpace(m.configPath) == "" {
		return nil
	}
	return terminaltheme.Save(m.configPath, id)
}

func (m *model) loadTheme(path string) {
	id, err := terminaltheme.Load(path)
	if err != nil {
		m.preferenceNotice(fmt.Errorf("theme preferences: %w", err))
		id = terminaltheme.Default
	}
	m.theme = id
	m.themePreview = ""
}

type themeColorKey struct{ r, g, b, a uint8 }

func colorKey(value color.Color) (themeColorKey, bool) {
	if value == nil {
		return themeColorKey{}, false
	}
	r, g, b, a := value.RGBA()
	return themeColorKey{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}, true
}

func themeColorMap(theme terminaltheme.Palette) map[themeColorKey]color.Color {
	base := renderPalette
	roles := []struct {
		from color.Color
		to   color.Color
	}{
		{base.Canvas, theme.Canvas}, {base.Panel, theme.Panel}, {base.Field, theme.Field},
		{base.Text, theme.Text}, {base.Muted, theme.Muted}, {base.Accent, theme.Accent},
		{base.Key, theme.Key}, {base.Rule, theme.Rule}, {base.SelectedForeground, theme.SelectedForeground},
		{base.SelectedBackground, theme.SelectedBackground}, {base.Error, theme.Error}, {base.Warning, theme.Warning},
		{base.ScrimBackground, theme.ScrimBackground}, {base.ScrimForeground, theme.ScrimForeground},
	}
	result := make(map[themeColorKey]color.Color, len(roles))
	for _, role := range roles {
		if from, ok := colorKey(role.from); ok {
			result[from] = role.to
		}
	}
	return result
}

// applyTheme remaps fully painted cells after compositing.
func applyTheme(content string, width, height int, id terminaltheme.ID) string {
	if id == renderThemeID || width < 1 || height < 1 {
		return content
	}
	theme := terminaltheme.Lookup(id)
	canvas := lipgloss.NewCanvas(width, height).Compose(lipgloss.NewLayer(content))
	colors := themeColorMap(theme)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			if replacement, ok := colors[colorKeyOrZero(cell.Style.Fg)]; ok {
				cell.Style.Fg = replacement
			}
			if replacement, ok := colors[colorKeyOrZero(cell.Style.Bg)]; ok {
				cell.Style.Bg = replacement
			}
			if replacement, ok := colors[colorKeyOrZero(cell.Style.UnderlineColor)]; ok {
				cell.Style.UnderlineColor = replacement
			}
		}
	}
	return canvas.Render()
}

func colorKeyOrZero(value color.Color) themeColorKey {
	key, _ := colorKey(value)
	return key
}
