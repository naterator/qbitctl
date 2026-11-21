//go:build !plan9

package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/naterator/qbitctl/internal/configfile"
)

// themeID is persisted in config.json. Keep these IDs stable: changing an ID
// would make a user's saved selection silently fall back to Default.
type themeID string

const defaultThemeID themeID = "default"

// themePalette contains the semantic colors used by the TUI. The renderer
// keeps the existing styles as the source of truth and remaps their cells at
// the final canvas boundary, so changing a theme cannot race global lipgloss
// state or leak into another TUI session.
type themePalette struct {
	id    themeID
	label string
	desc  string

	canvas     color.Color
	panel      color.Color
	field      color.Color
	text       color.Color
	muted      color.Color
	accent     color.Color
	key        color.Color
	rule       color.Color
	selectedFg color.Color
	selectedBg color.Color
	errorColor color.Color
	warn       color.Color
	scrimBg    color.Color
	scrimFg    color.Color
}

// themeCatalog is initialized once. The order is the order shown in the
// picker and should remain stable so the list feels familiar between releases.
var themeCatalog = []themePalette{
	makeTheme(defaultThemeID, "Default", "qbitctl's original palette",
		"#1D2531", "#334155", "#242F3F", "#E9EEF5", "#AFBCCD", "#85D7C8", "#9AC8FF", "#536478", "#102A2D", "#92DACD", "#FFAEA9", "#FFD18E", "#151C26", "#718195"),
	makeTheme("ocean", "Ocean", "Deep blue with bright cyan accents",
		"#0B1220", "#17233A", "#111D32", "#E5F2FF", "#8EA6C2", "#67E8F9", "#7DD3FC", "#355175", "#07121D", "#67E8F9", "#FB7185", "#FBCB7A", "#070B14", "#637D9D"),
	makeTheme("forest", "Forest", "Calm green terminal garden",
		"#0D1B16", "#17352A", "#122920", "#E8F5EC", "#98BCA7", "#7EE2A8", "#9BE7C2", "#3A6D55", "#092017", "#7EE2A8", "#FF9E9E", "#F6D487", "#09130F", "#648E78"),
	makeTheme("amber", "Amber", "Warm gold on dark brown",
		"#1F1710", "#382818", "#2B2116", "#FFF3DD", "#BDAE99", "#F4C95D", "#FFD89A", "#715531", "#2A1B06", "#F4C95D", "#FF9B8F", "#FFD17A", "#150E08", "#8B7558"),
	makeTheme("rose", "Rose", "Soft rose and plum",
		"#21141C", "#3B2431", "#2D1C27", "#FFF0F6", "#C2A6B5", "#F5A3C7", "#FFBCD8", "#70415C", "#29101E", "#F5A3C7", "#FF9B9B", "#FFD39A", "#160C12", "#91677D"),
	makeTheme("violet", "Violet", "Purple surfaces with cool highlights",
		"#171424", "#2A2140", "#211B35", "#F3EEFF", "#B1A6CB", "#C4A7FF", "#B8D5FF", "#5F4C89", "#21143B", "#C4A7FF", "#FF9FAA", "#FFDA8A", "#100C1A", "#80729F"),
	makeTheme("mono", "Monochrome", "Quiet grayscale with clear hierarchy",
		"#181A1F", "#2C3038", "#22262D", "#F2F4F7", "#AEB5C0", "#D3DBE6", "#FFFFFF", "#5C6472", "#1A1D23", "#D3DBE6", "#FFB4B4", "#E9C98E", "#101216", "#727A88"),
	makeTheme("nord", "Nord", "Arctic blue-gray",
		"#2E3440", "#3B4252", "#343C4C", "#ECEFF4", "#AAB4C3", "#8FBCBB", "#88C0D0", "#4C566A", "#1F2D37", "#8FBCBB", "#FF8F9C", "#EBCB8B", "#242933", "#6E798D"),
	makeTheme("dracula", "Dracula", "High-contrast purple night",
		"#1E1F29", "#282A36", "#232532", "#F8F8F2", "#A9A9B7", "#8BE9FD", "#BD93F9", "#6272A4", "#21142E", "#BD93F9", "#FF6E6E", "#FFB86C", "#15161E", "#687091"),
	makeTheme("solarized", "Solarized", "Classic teal and gold",
		"#002B36", "#073642", "#06313C", "#EEE8D5", "#93A1A1", "#5FD7CB", "#6CC3FF", "#586E75", "#002B36", "#B58900", "#FF6B66", "#F0C674", "#001F27", "#657B83"),
	makeTheme("gruvbox", "Gruvbox", "Earthy retro terminal colors",
		"#282828", "#3C3836", "#32302F", "#EBDBB2", "#D5C4A1", "#8EC07C", "#A8C9E8", "#665C54", "#282828", "#FABD2F", "#FF9280", "#FE8019", "#1D2021", "#928374"),
	makeTheme("catppuccin", "Catppuccin", "Pastel accents on mocha",
		"#1E1E2E", "#313244", "#242437", "#CDD6F4", "#A6ADC8", "#94E2D5", "#89B4FA", "#585B70", "#1E1E2E", "#CBA6F7", "#F38BA8", "#F9E2AF", "#11111B", "#7F849C"),
	makeTheme("high-contrast", "High contrast", "Maximum separation for accessibility",
		"#000000", "#1A1A1A", "#0E0E0E", "#FFFFFF", "#D0D0D0", "#00FFB3", "#4DB8FF", "#777777", "#000000", "#00FFB3", "#FF5F56", "#FFD75F", "#000000", "#999999"),
	makeTheme("paper", "Paper", "Light parchment with ink-blue accents",
		"#F4F1EA", "#E7E1D7", "#F0ECE4", "#252525", "#4E4A45", "#00645B", "#005FA8", "#B6AEA2", "#FFFFFF", "#00645B", "#A62828", "#8A5A00", "#D2CDC4", "#5C5852"),
	makeTheme("midnight", "Midnight", "Nearly black with electric cyan",
		"#0A0F1C", "#111A2E", "#0E1626", "#EBF3FF", "#8798B5", "#76E4F2", "#A9C7FF", "#2D476B", "#05131D", "#76E4F2", "#FF8F9C", "#FBD38D", "#060912", "#4E6487"),
	makeTheme("terminal", "Terminal", "Green phosphor with old-school contrast",
		"#001B0B", "#003219", "#002610", "#B8FFCB", "#5FCB7A", "#21FF7A", "#5CFFAF", "#1B7843", "#001B0B", "#21FF7A", "#FF7777", "#E5FF77", "#001006", "#3EA064"),
	makeTheme("sunset", "Sunset", "Coral light against plum",
		"#24151D", "#3D2333", "#2D1B28", "#FFF1E8", "#C4A2A8", "#FFB07C", "#FFCF93", "#75445A", "#2A1720", "#FFB07C", "#FF8C8C", "#FFE095", "#180D13", "#976E7B"),
}

func allThemes() []themePalette { return themeCatalog }

func makeTheme(id themeID, label, desc string, colors ...string) themePalette {
	if len(colors) != 14 {
		panic("qbitctl: theme palette requires fourteen colors")
	}
	toColor := func(value string) color.Color { return lipgloss.Color(value) }
	return themePalette{
		id: id, label: label, desc: desc,
		canvas: toColor(colors[0]), panel: toColor(colors[1]), field: toColor(colors[2]),
		text: toColor(colors[3]), muted: toColor(colors[4]), accent: toColor(colors[5]),
		key: toColor(colors[6]), rule: toColor(colors[7]), selectedFg: toColor(colors[8]),
		selectedBg: toColor(colors[9]), errorColor: toColor(colors[10]), warn: toColor(colors[11]),
		scrimBg: toColor(colors[12]), scrimFg: toColor(colors[13]),
	}
}

func themeByID(id themeID) themePalette {
	for _, theme := range allThemes() {
		if theme.id == id {
			return theme
		}
	}
	return allThemes()[0]
}

func knownTheme(id themeID) bool {
	for _, theme := range allThemes() {
		if theme.id == id {
			return true
		}
	}
	return false
}

func (m *model) activeThemeID() themeID {
	if knownTheme(m.themePreview) {
		return m.themePreview
	}
	if knownTheme(m.theme) {
		return m.theme
	}
	return defaultThemeID
}

// themeLabel reports the saved selection, rather than a temporary preview.
// Config views therefore never claim a scheme was saved before Enter was
// pressed in the picker.
func (m *model) themeLabel() string {
	return themeByID(m.theme).label
}

func themeIndex(id themeID) int {
	for index, theme := range allThemes() {
		if theme.id == id {
			return index
		}
	}
	return 0
}

// openThemePicker opens the same compact, keyboard-first list used by the
// existing sort/edit selectors. Arrow movement updates themePreview so the
// entire TUI previews the highlighted palette; Enter persists the stable ID.
func (m *model) openThemePicker() tea.Cmd {
	themes := allThemes()
	items := make([]dialogItem, 0, len(themes))
	for _, theme := range themes {
		items = append(items, dialogItem{
			label:       theme.label,
			action:      string(theme.id),
			description: theme.desc,
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
	id := themeID(m.dialog.items[index].action)
	if knownTheme(id) {
		m.themePreview = id
	}
}

// saveThemePreference writes a scalar ui.theme while leaving every unrelated
// config field byte-for-byte represented in the decoded JSON object intact.
func saveThemePreference(path string, id themeID) error {
	if !knownTheme(id) {
		id = defaultThemeID
	}
	return configfile.Update(path, func(root configfile.Object) (bool, error) {
		ui, err := root.Child("ui")
		if err != nil {
			return false, err
		}
		raw, err := json.Marshal(string(id))
		if err != nil {
			return false, err
		}
		if bytes.Equal(ui["theme"], raw) {
			return false, nil
		}
		ui["theme"] = raw
		encoded, err := json.Marshal(ui)
		if err != nil {
			return false, err
		}
		root["ui"] = encoded
		return true, nil
	})
}

func (m *model) persistTheme() error {
	if strings.TrimSpace(m.configPath) == "" {
		return nil
	}
	return saveThemePreference(m.configPath, m.theme)
}

func decodeTheme(raw json.RawMessage) (themeID, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return defaultThemeID, nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		if knownTheme(themeID(id)) {
			return themeID(id), nil
		}
		return defaultThemeID, nil
	}
	return defaultThemeID, json.Unmarshal(raw, &id)
}

func loadThemePreference(path string) (themeID, error) {
	if strings.TrimSpace(path) == "" {
		return defaultThemeID, nil
	}
	raw, err := readPreferences(path, "theme")
	if err != nil {
		return defaultThemeID, err
	}
	return decodeTheme(raw)
}

func (m *model) loadTheme(path string) {
	id, err := loadThemePreference(path)
	if err != nil {
		m.preferenceNotice(fmt.Errorf("theme preferences: %w", err))
		id = defaultThemeID
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

func themeColorMap(theme themePalette) map[themeColorKey]color.Color {
	base := themeByID(defaultThemeID)
	roles := []struct {
		from color.Color
		to   color.Color
	}{
		{base.canvas, theme.canvas}, {base.panel, theme.panel}, {base.field, theme.field},
		{base.text, theme.text}, {base.muted, theme.muted}, {base.accent, theme.accent},
		{base.key, theme.key}, {base.rule, theme.rule}, {base.selectedFg, theme.selectedFg},
		{base.selectedBg, theme.selectedBg}, {base.errorColor, theme.errorColor}, {base.warn, theme.warn},
		{base.scrimBg, theme.scrimBg}, {base.scrimFg, theme.scrimFg},
	}
	result := make(map[themeColorKey]color.Color, len(roles))
	for _, role := range roles {
		if from, ok := colorKey(role.from); ok {
			result[from] = role.to
		}
	}
	return result
}

// applyTheme remaps fully painted cells after compositing. Default returns
// the original string exactly, preserving the current palette as the
// compatibility baseline and avoiding needless ANSI parsing on every frame.
func applyTheme(content string, width, height int, id themeID) string {
	if id == "" || id == defaultThemeID || width < 1 || height < 1 {
		return content
	}
	theme := themeByID(id)
	if theme.id == defaultThemeID {
		return content
	}
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
