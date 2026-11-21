//go:build !plan9

package tui

import "charm.land/lipgloss/v2"

type helpShortcut struct {
	key   string
	label string
}

type helpGroup struct {
	title string
	items []helpShortcut
}

func (m *model) helpView(width, height int) []string {
	lines := helpLines(width)
	height = max(1, height)
	start := min(m.dialog.scroll, max(0, len(lines)-height))
	return lines[start:min(len(lines), start+height)]
}

func helpViewport(width, height int) (visible, count int) {
	return max(1, height), len(helpLines(width))
}

func helpLines(width int) []string {
	const gap = "   "
	columns := helpColumnCount(width)
	columnWidth := max(1, (width-(columns-1)*len(gap))/columns)
	lines := []string{viewTitleStyle.Render("Keyboard shortcuts"), ""}

	for start := 0; start < len(helpGroups); start += columns {
		groups := helpGroups[start:min(start+columns, len(helpGroups))]
		blocks := make([][]string, len(groups))
		height := 0
		for i, group := range groups {
			blocks[i] = helpGroupLines(group, columnWidth)
			height = max(height, len(blocks[i]))
		}
		for row := range height {
			line := ""
			for i, block := range blocks {
				if i > 0 {
					line += gap
				}
				value := ""
				if row < len(block) {
					value = block[row]
				}
				line += viewColumn(value, columnWidth)
			}
			lines = append(lines, line)
		}
		if start+columns < len(helpGroups) {
			lines = append(lines, "")
		}
	}
	return lines
}

func helpColumnCount(width int) int {
	switch {
	case width >= 96:
		return 3
	case width >= 62:
		return 2
	default:
		return 1
	}
}

func helpGroupLines(group helpGroup, width int) []string {
	keyWidth := 1
	for _, shortcut := range group.items {
		keyWidth = max(keyWidth, lipgloss.Width(shortcut.key))
	}
	keyWidth = min(keyWidth, max(1, width/2))
	lines := []string{viewTitleStyle.Render(viewFit(group.title, width))}
	for _, shortcut := range group.items {
		key := viewColumn(shortcut.key, keyWidth)
		label := viewFit(shortcut.label, width-keyWidth-2)
		lines = append(lines, viewKeyStyle.Bold(false).Render(key)+"  "+viewTextStyle.Render(label))
	}
	return lines
}

var helpGroups = []helpGroup{
	{
		title: "Navigate",
		items: []helpShortcut{
			{key: "↑/↓ · j/k", label: "Move"},
			{key: "pgup/pgdn", label: "Page"},
			{key: "home/end", label: "Top / bottom"},
			{key: "tab/⇧tab", label: "Switch tabs"},
			{key: "1/2/3/4", label: "Jump to a tab"},
		},
	},
	{
		title: "Config",
		items: []helpShortcut{
			{key: "enter", label: "Edit connection"},
			{key: "d", label: "Toggle TUI default"},
			{key: "c", label: "Choose color scheme"},
		},
	},
	{
		title: "Queue",
		items: []helpShortcut{
			{key: "/", label: "Filter queue"},
			{key: "o", label: "Sort queue"},
			{key: "enter", label: "Torrent details"},
			{key: "space", label: "Mark torrent"},
			{key: "esc", label: "Clear marks"},
			{key: "a", label: "Add torrent"},
		},
	},
	{
		title: "Torrent actions",
		items: []helpShortcut{
			{key: "s", label: "Start"},
			{key: "p", label: "Pause"},
			{key: "f", label: "Force start"},
			{key: "m", label: "Move"},
			{key: "t", label: "Trackers"},
			{key: "e", label: "Edit"},
			{key: "x", label: "Remove"},
			{key: "⇧d", label: "Delete files"},
		},
	},
	{
		title: "Search",
		items: []helpShortcut{
			{key: "/", label: "New search"},
			{key: "s", label: "Sort results"},
			{key: "f", label: "Filter results"},
			{key: "enter", label: "Add result"},
			{key: "esc", label: "Cancel / clear filter"},
		},
	},
	{
		title: "Dialogs",
		items: []helpShortcut{
			{key: "tab/⇧tab", label: "Change field"},
			{key: "enter", label: "Submit"},
			{key: "esc", label: "Cancel"},
		},
	},
	{
		title: "Session",
		items: []helpShortcut{
			{key: "ctrl+p · :", label: "Command palette"},
			{key: "?", label: "Keyboard shortcuts"},
			{key: "r", label: "Refresh"},
			{key: "ctrl+c", label: "Cancel search / quit"},
			{key: "q · ctrl+q", label: "Quit"},
		},
	},
}
