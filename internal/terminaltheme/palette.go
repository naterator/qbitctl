// Package terminaltheme owns the palettes and shared ui.theme preference.
package terminaltheme

import (
	"bytes"
	"encoding/json"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// ID is persisted in config.json. Keep these IDs stable: changing an ID
// would make a user's saved selection silently fall back to Default.
type ID string

const Default ID = "default"

// Palette contains the semantic colors used by the terminal interfaces.
type Palette struct {
	ID          ID
	Label       string
	Description string

	Canvas             color.Color
	Panel              color.Color
	Field              color.Color
	Text               color.Color
	Muted              color.Color
	Accent             color.Color
	Key                color.Color
	Rule               color.Color
	SelectedForeground color.Color
	SelectedBackground color.Color
	Error              color.Color
	Warning            color.Color
	ScrimBackground    color.Color
	ScrimForeground    color.Color
}

// themeCatalog is initialized once. The order is the order shown in the
// picker and should remain stable so the list feels familiar between releases.
var themeCatalog = []Palette{
	makeTheme(Default, "Default", "qbitctl's original palette",
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

func All() []Palette { return slices.Clone(themeCatalog) }

func makeTheme(id ID, label, desc string, colors ...string) Palette {
	if len(colors) != 14 {
		panic("qbitctl: theme palette requires fourteen colors")
	}
	toColor := func(value string) color.Color {
		rgb, err := strconv.ParseUint(value[1:], 16, 24)
		if err != nil {
			panic(err)
		}
		return color.RGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 255}
	}
	return Palette{
		ID: id, Label: label, Description: desc,
		Canvas: toColor(colors[0]), Panel: toColor(colors[1]), Field: toColor(colors[2]),
		Text: toColor(colors[3]), Muted: toColor(colors[4]), Accent: toColor(colors[5]),
		Key: toColor(colors[6]), Rule: toColor(colors[7]), SelectedForeground: toColor(colors[8]),
		SelectedBackground: toColor(colors[9]), Error: toColor(colors[10]), Warning: toColor(colors[11]),
		ScrimBackground: toColor(colors[12]), ScrimForeground: toColor(colors[13]),
	}
}

func Lookup(id ID) Palette {
	for _, theme := range themeCatalog {
		if theme.ID == id {
			return theme
		}
	}
	return themeCatalog[0]
}

func Known(id ID) bool {
	for _, theme := range themeCatalog {
		if theme.ID == id {
			return true
		}
	}
	return false
}

// Save writes a scalar ui.theme while leaving every unrelated
// config field byte-for-byte represented in the decoded JSON object intact.
func Save(path string, id ID) error {
	if !Known(id) {
		id = Default
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

func decode(raw json.RawMessage) (ID, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Default, nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return Default, err
	}
	return Lookup(ID(id)).ID, nil
}

func Load(path string) (ID, error) {
	if strings.TrimSpace(path) == "" {
		return Default, nil
	}
	root, err := configfile.Read(path)
	if err != nil {
		return Default, err
	}
	ui, err := root.Child("ui")
	if err != nil {
		return Default, err
	}
	return decode(ui["theme"])
}
