//go:build !plan9

package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m *model) openPaletteDialog() {
	search := dialogItem{label: "New search", action: "search", description: "Find torrents with Jackett"}
	if m.page != queuePage {
		search.keys = "/"
	}
	items := paletteGroup(nil, "Suggested",
		search,
		dialogItem{label: "Add torrent", action: "add", description: "Add a magnet, URL, or .torrent file", keys: "a"},
		dialogItem{label: "Refresh", action: "refresh", description: "Reload this page", keys: "r"},
	)
	items = paletteGroup(items, "Navigate",
		dialogItem{label: "Queue", action: "queue", description: "Browse and manage torrents", keys: "1"},
		dialogItem{label: "Search results", action: "search-page", description: "Show the latest search results", keys: "2"},
		dialogItem{label: "Server", action: "server", description: "View server information", keys: "3"},
		dialogItem{label: "Configuration", action: "config-page", description: "Edit connection and startup settings", keys: "4"},
	)
	switch m.page {
	case queuePage:
		items = paletteGroup(items, "Queue",
			dialogItem{label: "Filter queue", action: "filter", description: "Filter by name, hash, category, tags, or state", keys: "/"},
			dialogItem{label: "Sort queue", action: "sort-queue", description: "Sort by name, progress, or state", keys: "o"},
			dialogItem{label: "Torrent details", action: "details", description: "Inspect the selected torrent", keys: "enter"},
			dialogItem{label: "Trackers", action: "trackers", description: "Inspect tracker status", keys: "t"},
			dialogItem{label: "Edit torrent", action: "edit", description: "Set a qBittorrent torrent field", keys: "e"},
			dialogItem{label: "Clear marks", action: "clear-marks", description: "Clear multi-select marks", keys: "esc"},
		)
		items = paletteGroup(items, "Transfer actions",
			dialogItem{label: "Start", action: "start", description: "Start selected torrents", keys: "s"},
			dialogItem{label: "Pause", action: "pause", description: "Pause selected torrents", keys: "p"},
			dialogItem{label: "Force start", action: "force-start", description: "Force selected torrents to start", keys: "f"},
			dialogItem{label: "Move", action: "move", description: "Change the selected torrent path", keys: "m"},
			dialogItem{label: "Remove", action: "remove", description: "Remove torrents but keep files", keys: "x"},
			dialogItem{label: "Delete files", action: "delete", description: "Remove torrents and delete files", keys: "⇧d"},
		)
	case searchPage:
		items = paletteGroup(items, "Current results",
			dialogItem{label: "Filter results", action: "filter-results", description: "Narrow loaded results by text, seeders, or size", keys: "f"},
			dialogItem{label: "Sort results", action: "sort-results", description: "Sort the current results by seeders or size", keys: "s"},
			dialogItem{label: "Add selected result", action: "add-result", description: "Submit a cached search result", keys: "enter"},
		)
	}
	items = paletteGroup(items, "Session",
		dialogItem{label: "Keyboard shortcuts", action: "help", description: "Show help and key bindings", keys: "?"},
		dialogItem{label: "Quit", action: "quit", description: "Close qbitctl", keys: "q"},
	)

	d := &dialog{
		kind:     dialogPalette,
		title:    "Command palette",
		fields:   []dialogField{newDialogField("", "", "Find a command", "")},
		allItems: items,
		items:    append([]dialogItem(nil), items...),
	}
	m.dialog = d
	d.focusField(0)
}

func paletteGroup(items []dialogItem, group string, commands ...dialogItem) []dialogItem {
	for _, command := range commands {
		command.group = group
		items = append(items, command)
	}
	return items
}

func (d *dialog) refreshPalette() {
	if d == nil || d.kind != dialogPalette || len(d.fields) == 0 {
		return
	}
	query := strings.ToLower(strings.TrimSpace(d.value(0)))
	selectedAction := ""
	if d.selected >= 0 && d.selected < len(d.items) {
		selectedAction = d.items[d.selected].action
	}
	d.items = d.items[:0]
	for _, item := range d.allItems {
		if query == "" || strings.Contains(strings.ToLower(item.label), query) ||
			strings.Contains(strings.ToLower(item.action), query) ||
			strings.Contains(strings.ToLower(item.description), query) ||
			strings.Contains(strings.ToLower(item.keys), query) {
			d.items = append(d.items, item)
		}
	}
	d.selected = 0
	for index, item := range d.items {
		if item.action == selectedAction {
			d.selected = index
			break
		}
	}
}

// dialogPaletteLines keeps the filter visible as the selection moves through
// command groups. Captions take up display rows without taking keyboard focus.
func (m *model) dialogPaletteLines(width, height int) []string {
	d := m.dialog
	field := d.fields[0].input
	field.SetWidth(max(1, width-3))
	filter := viewAccentStyle.Render("› ") + field.View()
	header := []string{viewSurface(viewFit(filter, width), width, 1, viewFieldColor, viewTextStyle.GetForeground())}
	if height >= 6 {
		header = append(header, "")
	}
	visible := height - len(header)
	if visible < 1 {
		return header
	}
	if len(d.items) == 0 {
		return append(header, viewMutedStyle.Render("No matching commands"))
	}

	lines := make([]string, 0, len(d.items))
	previousGroup := ""
	selectedRow := 0
	for i, item := range d.items {
		if item.group != previousGroup {
			if previousGroup != "" && visible >= 13 {
				lines = append(lines, "")
			}
			lines = append(lines, viewAccentStyle.Bold(true).Render(viewFit("  "+item.group, width)))
			previousGroup = item.group
		}
		if i == d.selected {
			selectedRow = len(lines)
		}
		lines = append(lines, paletteCommandLine(item, i == d.selected, width))
	}
	start := min(max(0, selectedRow-visible/2), max(0, len(lines)-visible))
	return append(header, lines[start:min(len(lines), start+visible)]...)
}

func paletteCommandLine(item dialogItem, selected bool, width int) string {
	label := "  " + item.label
	if selected {
		label = "› " + item.label
	}
	if width < 16 {
		return paletteSelection(label, selected, width)
	}
	keys := item.keys
	label = viewFit(label, width-lipgloss.Width(keys)-1)
	if selected {
		return paletteSelection(viewRightAligned(width, label, keys), true, width)
	}
	return viewRightAligned(width, viewTextStyle.Render(label), viewMutedStyle.Render(keys))
}

func paletteSelection(line string, selected bool, width int) string {
	line = viewFit(line, width)
	if selected {
		return viewSelectedSty.Width(width).Render(line)
	}
	return viewTextStyle.Render(line)
}
