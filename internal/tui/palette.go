//go:build !plan9

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m *model) openPaletteDialog() {
	items := m.paletteCommands()
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

// paletteCommands returns the commands that can be acted on from the current
// model state. The palette is deliberately rebuilt on every open (and by the
// model after asynchronous work completes), so it does not advertise stale
// queue selections, search results, or provider catalogs.
func (m *model) paletteCommands() []dialogItem {
	connected := m.backend != nil
	idle := m.busy == "" && !m.quitting
	canRequest := connected && idle

	items := make([]dialogItem, 0, 32)
	if canRequest {
		searchKeys := ""
		if m.page != queuePage {
			searchKeys = "/"
		}
		items = paletteGroup(items, "Suggested",
			dialogItem{label: "New search", action: "search", description: "Find torrents with " + m.searchPluginLabel(m.searchPlugin), keys: searchKeys},
			dialogItem{label: "Add torrent", action: "add", description: "Add a magnet, URL, or .torrent file", keys: "a"},
		)
	}

	// Navigation itself remains useful while disconnected: Config is needed to
	// establish a connection, and the other tabs provide a predictable place
	// to return after a connection finishes.
	items = paletteGroup(items, "Navigate",
		dialogItem{label: "Queue", action: "queue", description: "Browse and manage torrents", keys: "1"},
		dialogItem{label: "Search results", action: "search-page", description: "Show the latest search results", keys: "2"},
		dialogItem{label: "Server", action: "server", description: "View server information", keys: "3"},
		dialogItem{label: "Configuration", action: "config-page", description: "Edit connection, view, and search settings", keys: "4"},
	)

	switch m.page {
	case queuePage:
		m.addQueuePaletteCommands(&items, connected, idle)
	case searchPage:
		m.addSearchPaletteCommands(&items, connected, idle)
	case serverPage:
		m.addServerPaletteCommands(&items, connected, idle)
	case configPage:
		m.addConfigPaletteCommands(&items, idle)
	}
	if m.busy != "" && m.work == searchWork {
		items = paletteGroup(items, "Search",
			dialogItem{label: "Cancel search", action: "cancel-search", description: "Stop the active search request", keys: "esc"},
		)
	}
	if m.busy != "" && m.work == searchPluginManageWork {
		items = paletteGroup(items, "Search plugins",
			dialogItem{label: "Cancel plugin request", action: "cancel-plugin-request", description: "Stop the active search-plugin request", keys: "esc"},
		)
	}
	if m.busy != "" && m.work == connectWork {
		items = paletteGroup(items, "Connection",
			dialogItem{label: "Cancel connection", action: "cancel-connection", description: "Stop the connection attempt", keys: "esc"},
		)
	}

	if connected {
		label := "Pause automatic refresh"
		if m.refreshPaused {
			label = "Resume automatic refresh"
		}
		items = paletteGroup(items, "Session",
			dialogItem{label: label, action: "toggle-refresh", description: "Control periodic queue, details, and server updates"},
		)
	}
	items = paletteGroup(items, "Session",
		dialogItem{label: "Keyboard shortcuts", action: "help", description: "Show help and key bindings", keys: "?"},
		dialogItem{label: "Quit", action: "quit", description: "Close qbitctl", keys: "q"},
	)
	return items
}

func (m *model) addQueuePaletteCommands(items *[]dialogItem, connected, idle bool) {
	if idle {
		*items = paletteGroup(*items, "Queue",
			dialogItem{label: "Filter queue", action: "filter", description: "Filter by name, hash, category, tags, or state", keys: "/"},
			dialogItem{label: "Sort queue", action: "sort-queue", description: "Sort by size, ETA, speed, and other torrent fields", keys: "o"},
		)
		if connected {
			*items = paletteGroup(*items, "Queue",
				dialogItem{label: "Refresh queue", action: "refresh", description: "Reload the torrent queue", keys: "r"},
			)
		}
	}
	if idle && (strings.TrimSpace(m.filter) != "" || (strings.TrimSpace(m.stateFilter) != "" && !strings.EqualFold(strings.TrimSpace(m.stateFilter), "all"))) {
		*items = paletteGroup(*items, "Queue",
			dialogItem{label: "Clear queue filter", action: "clear-queue-filter", description: "Show all torrents again", keys: ""},
		)
	}
	if !idle {
		return
	}

	focused, hasFocused := m.focusedTorrent()
	if hasFocused {
		label, description := "Mark torrent", "Mark the focused torrent for multi-select actions"
		if m.marked[focused.Hash] {
			label, description = "Unmark torrent", "Remove the focused torrent from the multi-select set"
		}
		*items = paletteGroup(*items, "Queue",
			dialogItem{label: label, action: "toggle-mark", description: description, keys: "space"},
		)
	}
	if len(m.marked) > 0 {
		*items = paletteGroup(*items, "Queue",
			dialogItem{label: "Clear marks", action: "clear-marks", description: "Clear all marked torrents", keys: "esc"},
		)
	}
	if len(m.queue) > 0 {
		m.addListMovementCommands(items, len(m.queue))
	}
	if !connected {
		return
	}
	targets := m.targetHashes()
	if hasFocused {
		*items = paletteGroup(*items, "Queue",
			dialogItem{label: "Torrent details", action: "details", description: "Inspect the selected torrent, files, and peers", keys: "enter"},
			dialogItem{label: "Trackers", action: "trackers", description: "Inspect status or edit trackers", keys: "t"},
		)
	}
	if len(targets) == 0 {
		return
	}
	*items = paletteGroup(*items, "Transfer actions",
		dialogItem{label: "Edit torrent", action: "edit", description: "Set a qBittorrent torrent field", keys: "e"},
		dialogItem{label: "Start", action: "start", description: "Start the focused or marked torrents", keys: "s"},
		dialogItem{label: "Pause", action: "pause", description: "Pause the focused or marked torrents", keys: "p"},
		dialogItem{label: "Force start", action: "force-start", description: "Force the focused or marked torrents to start", keys: "f"},
		dialogItem{label: "Move", action: "move", description: "Change the selected torrent path", keys: "m"},
		dialogItem{label: "Remove", action: "remove", description: "Remove torrents but keep files", keys: "x"},
		dialogItem{label: "Delete files", action: "delete", description: "Remove torrents and delete files", keys: "⇧d"},
	)
	actions := []dialogItem{
		{label: "Recheck", action: "recheck", description: "Verify the focused or marked torrent data"},
		{label: "Reannounce", action: "reannounce", description: "Ask trackers to announce the focused or marked torrents"},
		{label: "Queue up", action: "queue-up", description: "Move the focused or marked torrents up one queue position"},
		{label: "Queue down", action: "queue-down", description: "Move the focused or marked torrents down one queue position"},
		{label: "Queue top", action: "queue-top", description: "Move the focused or marked torrents to the top of the queue"},
		{label: "Queue bottom", action: "queue-bottom", description: "Move the focused or marked torrents to the bottom of the queue"},
	}
	if len(targets) == 1 {
		actions = append(actions, dialogItem{label: "Rename torrent", action: "rename", description: "Rename the focused torrent"})
	}
	*items = paletteGroup(*items, "Torrent actions", actions...)

}

func (m *model) addSearchPaletteCommands(items *[]dialogItem, connected, idle bool) {
	// Provider discovery is intentionally allowed while a search runs; the
	// picker changes only the next search and does not disturb current results.
	if connected {
		*items = paletteGroup(*items, "Search",
			dialogItem{label: "Choose search provider", action: "providers", description: "Choose a provider or manage search plugins", keys: "p"},
		)
	}
	if !idle {
		return
	}
	if connected && strings.TrimSpace(m.query) != "" {
		*items = paletteGroup(*items, "Search",
			dialogItem{label: "Search again", action: "refresh", description: "Run the current query again", keys: "r"},
		)
	}
	// Keep these visible even before the first result arrives: they configure
	// the next Search interaction and are useful when a previous result set is
	// empty or still being rebuilt.
	*items = paletteGroup(*items, "Search results",
		dialogItem{label: "Filter results", action: "filter-results", description: "Narrow loaded results by text, seeders, or size", keys: "f"},
		dialogItem{label: "Sort results", action: "sort-results", description: "Sort the current results by seeders or size", keys: "s"},
	)
	if m.resultFilter.active() {
		*items = paletteGroup(*items, "Search results",
			dialogItem{label: "Clear result filter", action: "clear-result-filter", description: "Show all search results again", keys: ""},
		)
	}
	if connected {
		label := "Add result by ID"
		description := "Submit a cached search result by its numeric ID"
		keys := ""
		if result, ok := m.focusedResult(); ok && result.ID > 0 {
			label = "Add selected result"
			description = fmt.Sprintf("Submit cached result %d", result.ID)
			keys = "enter"
		}
		*items = paletteGroup(*items, "Search results",
			dialogItem{label: label, action: "add-result", description: description, keys: keys},
		)
	}
	if len(m.results) > 0 {
		m.addListMovementCommands(items, len(m.results))
	}
}

func (m *model) addServerPaletteCommands(items *[]dialogItem, connected, idle bool) {
	if !connected || !idle {
		return
	}
	*items = paletteGroup(*items, "Server",
		dialogItem{label: "Refresh server info", action: "refresh", description: "Reload qBittorrent server and transfer statistics", keys: "r"},
	)
	if len(m.serverRows) > 0 {
		*items = paletteGroup(*items, "Server",
			dialogItem{label: "Server details", action: "server-details", description: "Inspect the loaded server statistics", keys: "enter"},
		)
	}
}

func (m *model) addConfigPaletteCommands(items *[]dialogItem, idle bool) {
	if !idle {
		return
	}
	*items = paletteGroup(*items, "Connection",
		dialogItem{label: "Edit server address", action: "config-server", description: "Change the qBittorrent Web API address"},
		dialogItem{label: "Edit username", action: "config-username", description: "Change the Web API username"},
		dialogItem{label: "Edit password", action: "config-password", description: "Change the saved Web API password"},
	)
	*items = paletteGroup(*items, "View",
		dialogItem{label: "Toggle TUI default", action: "config-default", description: "Choose whether qbitctl starts in the TUI"},
		dialogItem{label: "Choose color scheme", action: "config-theme", description: "Preview and save a color scheme"},
		dialogItem{label: "Toggle data units", action: "config-units", description: "Switch between human-readable and exact bytes"},
	)
	*items = paletteGroup(*items, "Search",
		dialogItem{label: "Choose default provider", action: "config-provider", description: "Save the provider used by new searches"},
	)
	*items = paletteGroup(*items, "Files",
		dialogItem{label: "Show config file path", action: "config-file", description: "Focus the saved configuration file path"},
		dialogItem{label: "Show search cache path", action: "config-cache", description: "Focus the local cached search-results path"},
	)
	m.addListMovementCommands(items, len(m.configSettings()))
}

func (m *model) addListMovementCommands(items *[]dialogItem, count int) {
	if count < 1 {
		return
	}
	*items = paletteGroup(*items, "Navigate list",
		dialogItem{label: "Previous item", action: "previous-item", description: "Move to the previous visible item", keys: "↑/k"},
		dialogItem{label: "Next item", action: "next-item", description: "Move to the next visible item", keys: "↓/j"},
		dialogItem{label: "Previous page", action: "previous-page", description: "Move up one page of items", keys: "pgup"},
		dialogItem{label: "Next page", action: "next-page", description: "Move down one page of items", keys: "pgdn"},
		dialogItem{label: "First item", action: "first-item", description: "Jump to the first visible item", keys: "home/g"},
		dialogItem{label: "Last item", action: "last-item", description: "Jump to the last visible item", keys: "end/G"},
	)
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
			strings.Contains(strings.ToLower(item.group), query) ||
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
