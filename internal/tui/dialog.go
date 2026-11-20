//go:build !plan9

package tui

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

// dialogKind identifies the small, modal interactions used by the TUI.  The
// dialog deliberately owns its widgets: the queue, search, and server pages
// remain useful while a dialog is open, but do not need to know how a form is
// laid out or how its focus moves.
type dialogKind string

const (
	dialogPalette    dialogKind = "palette"
	dialogSearch     dialogKind = "search"
	dialogFilter     dialogKind = "filter"
	dialogAdd        dialogKind = "add"
	dialogAddResult  dialogKind = "add-result"
	dialogEditSelect dialogKind = "edit-select"
	dialogResultSort dialogKind = "result-sort"
	dialogQueueSort  dialogKind = "queue-sort"
	dialogEditValue  dialogKind = "edit-value"
	dialogMove       dialogKind = "move"
	dialogConfirm    dialogKind = "confirm"
	dialogDelete     dialogKind = "delete"
	dialogDetails    dialogKind = "details"
	dialogHelp       dialogKind = "help"
)

type dialogField struct {
	label       string
	help        string
	placeholder string
	input       textinput.Model
}

type dialogItem struct {
	label       string
	action      string
	description string
	keys        string
}

type dialog struct {
	kind  dialogKind
	title string

	fields []dialogField
	focus  int

	// Palette dialogs keep the unfiltered list so typing can cheaply rebuild
	// the visible list without losing the action context.
	allItems []dialogItem
	items    []dialogItem
	selected int

	rows   []detailRow
	scroll int

	request actionRequest
	// torrent is captured when the edit dialog opens. Setters intentionally
	// edit only that focused torrent, even when other rows are marked.
	torrent *client.TorrentInfo
	status  string
	message string
}

func newDialogField(label, value, placeholder, help string) dialogField {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.SetValue(value)
	return dialogField{
		label:       label,
		help:        help,
		placeholder: placeholder,
		input:       input,
	}
}

func (d *dialog) value(index int) string {
	if d == nil || index < 0 || index >= len(d.fields) {
		return ""
	}
	return d.fields[index].input.Value()
}

func (d *dialog) focusField(index int) tea.Cmd {
	if d == nil || len(d.fields) == 0 {
		return nil
	}
	if index < 0 {
		index = len(d.fields) - 1
	}
	if index >= len(d.fields) {
		index = 0
	}
	for i := range d.fields {
		d.fields[i].input.Blur()
	}
	d.focus = index
	return d.fields[d.focus].input.Focus()
}

func (d *dialog) cycleField(backward bool) tea.Cmd {
	if d == nil || len(d.fields) == 0 {
		return nil
	}
	next := d.focus + 1
	if backward {
		next = d.focus - 1
	}
	if next < 0 {
		next = len(d.fields) - 1
	}
	if next >= len(d.fields) {
		next = 0
	}
	return d.focusField(next)
}

func (d *dialog) refreshPalette() {
	if d == nil || d.kind != dialogPalette || len(d.fields) == 0 {
		return
	}
	query := strings.ToLower(strings.TrimSpace(d.value(0)))
	d.items = d.items[:0]
	for _, item := range d.allItems {
		if query == "" || strings.Contains(strings.ToLower(item.label), query) ||
			strings.Contains(strings.ToLower(item.action), query) ||
			strings.Contains(strings.ToLower(item.description), query) ||
			strings.Contains(strings.ToLower(item.keys), query) {
			d.items = append(d.items, item)
		}
	}
	if len(d.items) == 0 {
		d.selected = 0
		return
	}
	if d.selected >= len(d.items) {
		d.selected = len(d.items) - 1
	}
	if d.selected < 0 {
		d.selected = 0
	}
}

func (d *dialog) stepSelection(delta int) {
	if d == nil || len(d.items) == 0 {
		return
	}
	d.selected += delta
	if d.selected < 0 {
		d.selected = len(d.items) - 1
	}
	if d.selected >= len(d.items) {
		d.selected = 0
	}
}

func (d *dialog) pageSelection(delta int) {
	if d == nil || len(d.items) == 0 {
		return
	}
	step := 8
	d.selected += delta * step
	if d.selected < 0 {
		d.selected = 0
	}
	if d.selected >= len(d.items) {
		d.selected = len(d.items) - 1
	}
}

func (d *dialog) stepScroll(delta, visible, count int) {
	if count == 0 {
		return
	}
	if visible < 1 {
		visible = 1
	}
	d.scroll += delta
	maxScroll := count - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scroll < 0 {
		d.scroll = 0
	}
	if d.scroll > maxScroll {
		d.scroll = maxScroll
	}
}

func (m *model) openAction(action string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "palette", "command":
		m.openPaletteDialog()
		return nil
	case "queue":
		m.dialog = nil
		return m.goTo(queuePage)
	case "search-page", "searchpage":
		m.dialog = nil
		return m.goTo(searchPage)
	case "server", "server-page":
		m.dialog = nil
		return m.goTo(serverPage)
	case "search":
		m.openSearchDialog()
		return nil
	case "filter":
		m.openFilterDialog()
		return nil
	case "filter-results":
		if m.page != searchPage {
			return nil
		}
		return m.startResultFilter()
	case "sort-results":
		if m.page == searchPage {
			m.openSortDialog(dialogResultSort)
		}
		return nil
	case "sort-queue":
		if m.page == queuePage {
			m.openSortDialog(dialogQueueSort)
		}
		return nil
	case "details":
		m.dialog = nil
		return m.loadDetails(false)
	case "trackers":
		m.dialog = nil
		return m.loadDetails(true)
	case "add":
		m.openAddDialog()
		return nil
	case "add-result", "add result":
		m.openAddResultDialog()
		return nil
	case "edit":
		m.openEditDialog()
		return nil
	case "start", "pause", "force-start":
		hashes := m.targetHashes()
		if len(hashes) == 0 {
			m.notice = "Select or mark a torrent first"
			return nil
		}
		if m.busy != "" {
			m.notice = "Finish the current request first"
			return nil
		}
		m.dialog = nil
		return m.launchAction(action, actionRequest{kind: action, hashes: joinHashes(hashes)})
	case "move":
		if len(m.targetHashes()) == 0 {
			m.notice = "Select or mark a torrent first"
			return nil
		}
		m.openMoveDialog()
		return nil
	case "remove":
		return m.openRemoveDialog(false)
	case "delete":
		return m.openRemoveDialog(true)
	case "refresh":
		m.dialog = nil
		return m.refresh()
	case "clear-marks", "clear marks":
		m.clearMarks()
		m.dialog = nil
		return nil
	case "help":
		m.openHelpDialog()
		return nil
	case "quit", "exit":
		m.dialog = nil
		return m.requestQuit()
	default:
		return nil
	}
}

func (m *model) openPaletteDialog() {
	items := []dialogItem{
		{label: "Queue", action: "queue", description: "Browse and manage torrents", keys: "1"},
		{label: "Search", action: "search", description: "Search Jackett and qBittorrent"},
		{label: "Search page", action: "search-page", description: "Show the latest search results", keys: "2"},
		{label: "Server", action: "server", description: "View qBittorrent server information", keys: "3"},
		{label: "Add torrent", action: "add", description: "Add a magnet, URL, or .torrent file", keys: "a"},
		{label: "Refresh", action: "refresh", description: "Reload the current page", keys: "r"},
		{label: "Help", action: "help", description: "Show keyboard shortcuts", keys: "?"},
		{label: "Quit", action: "quit", description: "Close qbitctl", keys: "q"},
	}
	if m.page == searchPage {
		for i := range items {
			if items[i].action == "search" {
				items[i].keys = "/"
				break
			}
		}
	}
	if m.page == queuePage {
		items = append(items,
			dialogItem{label: "Filter queue", action: "filter", description: "Filter by name, hash, category, tags, or state", keys: "/"},
			dialogItem{label: "Sort queue", action: "sort-queue", description: "Sort by name, progress, or state", keys: "o"},
			dialogItem{label: "Torrent details", action: "details", description: "Inspect the selected torrent", keys: "enter"},
			dialogItem{label: "Trackers", action: "trackers", description: "Inspect tracker status", keys: "t"},
			dialogItem{label: "Edit torrent", action: "edit", description: "Set a qBittorrent torrent field", keys: "e"},
			dialogItem{label: "Start", action: "start", description: "Start selected torrents", keys: "s"},
			dialogItem{label: "Pause", action: "pause", description: "Pause selected torrents", keys: "p"},
			dialogItem{label: "Force start", action: "force-start", description: "Force selected torrents to start", keys: "f"},
			dialogItem{label: "Move", action: "move", description: "Change the selected torrent path", keys: "m"},
			dialogItem{label: "Remove", action: "remove", description: "Remove torrents but keep files", keys: "x"},
			dialogItem{label: "Delete files", action: "delete", description: "Remove torrents and delete files", keys: "D"},
			dialogItem{label: "Clear marks", action: "clear-marks", description: "Clear multi-select marks", keys: "Esc"},
		)
	}
	if m.page == searchPage {
		items = append(items,
			dialogItem{label: "Filter results", action: "filter-results", description: "Narrow loaded results by text, seeders, or size", keys: "f"},
			dialogItem{label: "Sort results", action: "sort-results", description: "Sort the current results by seeders or size", keys: "s"},
			dialogItem{label: "Add selected result", action: "add-result", description: "Submit a cached search result", keys: "enter"},
		)
	}
	d := &dialog{
		kind:     dialogPalette,
		title:    "Command palette",
		fields:   []dialogField{newDialogField("Filter", "", "type to filter commands", "")},
		allItems: items,
		items:    append([]dialogItem(nil), items...),
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openSearchDialog() {
	plugin := strings.TrimSpace(m.searchPlugin)
	if plugin == "" {
		plugin = "jackett"
	}
	timeout := m.searchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	d := &dialog{
		kind:  dialogSearch,
		title: "Search Jackett",
		fields: []dialogField{
			newDialogField("Query", m.query, "Ubuntu 24.04", "The torrent search phrase"),
			newDialogField("Plugin", plugin, "jackett", "qBittorrent search plugin"),
			newDialogField("Timeout", timeout.String(), "30s", "Go duration, for example 45s or 2m"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openFilterDialog() {
	d := &dialog{
		kind:  dialogFilter,
		title: "Filter queue",
		fields: []dialogField{
			newDialogField("Query", m.filter, "name, hash, category, or tags", "Matches torrent name, hash, category, and tags"),
			newDialogField("State", m.stateFilter, "all, active, downloading, seeding, paused", "Optional state token; all clears it"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openAddDialog() {
	d := &dialog{
		kind:  dialogAdd,
		title: "Add torrent",
		fields: []dialogField{
			newDialogField("Source", "", "magnet, URL, or .torrent path", "Local torrent files are uploaded to qBittorrent"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openAddResultDialog() {
	value := ""
	if result, ok := m.focusedResult(); ok && result.ID > 0 {
		value = strconv.FormatInt(result.ID, 10)
	}
	d := &dialog{
		kind:  dialogAddResult,
		title: "Add search result",
		fields: []dialogField{
			newDialogField("Result ID", value, "numeric ID from the search page", "Only IDs from the local latest-search cache are accepted"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openEditDialog() {
	torrent, ok := m.focusedTorrent()
	if !ok {
		m.notice = "Select or mark a torrent first"
		return
	}
	items := make([]dialogItem, 0, len(client.SetFieldNames))
	for _, field := range client.SetFieldNames {
		items = append(items, dialogItem{label: field, action: field, description: "Set this qBittorrent field"})
	}
	d := &dialog{
		kind:     dialogEditSelect,
		title:    "Edit torrent field",
		allItems: items,
		items:    append([]dialogItem(nil), items...),
		torrent:  &torrent,
		request:  actionRequest{hashes: torrent.Hash},
	}
	m.dialog = d
}

func (m *model) openSortDialog(kind dialogKind) {
	title, current := "Sort search results", string(m.resultSort)
	var items []dialogItem
	if kind == dialogQueueSort {
		title, current = "Sort queue", string(m.queueSort)
		for _, choice := range queueSortChoices {
			items = append(items, dialogItem{label: choice.label, action: string(choice.order)})
		}
	} else {
		for _, choice := range resultSortChoices {
			items = append(items, dialogItem{label: choice.label, action: string(choice.order)})
		}
	}
	selected := 0
	for i, item := range items {
		if item.action == current {
			selected = i
			break
		}
	}
	m.dialog = &dialog{
		kind:     kind,
		title:    title,
		items:    items,
		selected: selected,
	}
}

func (m *model) openMoveDialog() {
	hashes := m.targetHashes()
	if len(hashes) == 0 {
		m.notice = "Select or mark a torrent first"
		return
	}
	d := &dialog{
		kind:    dialogMove,
		title:   "Move torrent",
		request: actionRequest{kind: "move", hashes: joinHashes(hashes)},
		fields: []dialogField{
			newDialogField("Path", "", "/downloads/path", "Destination directory on the qBittorrent host"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openRemoveDialog(deleteFiles bool) tea.Cmd {
	hashes := m.targetHashes()
	if len(hashes) == 0 {
		m.notice = "Select or mark a torrent first"
		return nil
	}
	if deleteFiles {
		d := &dialog{
			kind:    dialogDelete,
			title:   "Delete torrents and files",
			message: selectionMessage(m, hashes, true),
			fields: []dialogField{
				newDialogField("Confirmation", "", "DELETE", "This safeguard is case-sensitive"),
			},
			request: actionRequest{kind: "delete", hashes: joinHashes(hashes)},
		}
		m.dialog = d
		d.focusField(0)
		return nil
	}
	d := &dialog{
		kind:    dialogConfirm,
		title:   "Remove torrents",
		message: selectionMessage(m, hashes, false),
		request: actionRequest{kind: "remove", hashes: joinHashes(hashes)},
	}
	m.dialog = d
	return nil
}

func selectionMessage(m *model, hashes []string, deleteFiles bool) string {
	verb := "Remove"
	suffix := "Downloaded files will be kept."
	if deleteFiles {
		verb = "Delete"
		suffix = "This permanently deletes the selected torrent files. Type DELETE to continue."
	}
	count := len(hashes)
	noun := "torrents"
	if count == 1 {
		noun = "torrent"
	}
	lines := []string{verb + " " + strconv.Itoa(count) + " " + noun + "?"}
	nameByHash := make(map[string]string, len(m.allTorrents)+len(m.queue))
	for _, torrent := range append(append([]client.TorrentInfo(nil), m.allTorrents...), m.queue...) {
		if _, exists := nameByHash[torrent.Hash]; !exists {
			nameByHash[torrent.Hash] = viewSafe(strings.TrimSpace(torrent.Name))
		}
	}
	limit := min(3, len(hashes))
	for _, hash := range hashes[:limit] {
		name := nameByHash[hash]
		if name == "" {
			name = hash
		}
		lines = append(lines, "  · "+name)
	}
	if len(hashes) > limit {
		lines = append(lines, "  · +"+strconv.Itoa(len(hashes)-limit)+" more")
	}
	lines = append(lines, suffix)
	return strings.Join(lines, "\n")
}

func (m *model) openHelpDialog() {
	rows := []detailRow{
		{label: "Navigation", value: "↑/↓ or j/k move · PgUp/PgDn scroll · Tab changes page"},
		{label: "Palette", value: "Ctrl+P or : opens commands; type to filter; Enter runs"},
		{label: "Queue", value: "Space marks · Enter details · a add · e edit · p pause · s start"},
		{label: "Queue", value: "f force-start · m move · x remove · D delete files · Esc marks"},
		{label: "Queue sort", value: "o sorts by name, progress, or state · Active first restores the default"},
		{label: "Filter", value: "/ queue filter · name/hash/category/tags · state all/active/etc."},
		{label: "Search", value: "/ new search · f filter current results · s sort results · Enter add selected result"},
		{label: "Result filters", value: "Tab between text, minimum seeders, and size bounds. Enter keeps filters, Esc cancels edits."},
		{label: "Dialogs", value: "Tab/Shift+Tab fields · Enter submit · Esc cancel"},
		{label: "Safety", value: "Delete files requires typing DELETE; actions target marks first"},
		{label: "Global", value: "q quits · Ctrl+C cancels search or exits"},
	}
	m.openDetailDialog("qbitctl TUI help", rows)
	if m.dialog != nil {
		m.dialog.kind = dialogHelp
	}
}

func (m *model) openDetailDialog(title string, rows []detailRow) {
	copyRows := append([]detailRow(nil), rows...)
	m.dialog = &dialog{kind: dialogDetails, title: title, rows: copyRows}
}

func (m *model) updateDialog(msg tea.Msg) tea.Cmd {
	d := m.dialog
	if d == nil {
		return nil
	}

	if paste, ok := msg.(tea.PasteMsg); ok {
		if len(d.fields) > 0 && d.kind != dialogConfirm && d.kind != dialogDelete && d.kind != dialogEditSelect && d.kind != dialogDetails && d.kind != dialogHelp {
			var cmd tea.Cmd
			d.fields[d.focus].input, cmd = d.fields[d.focus].input.Update(paste)
			if d.kind == dialogPalette {
				d.refreshPalette()
			}
			return cmd
		}
		return nil
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	name := key.String()

	// Ctrl+C is intentionally handled by the parent model. This keeps one
	// cancellation policy for in-flight requests and for the interactive UI.
	if name == "esc" || name == "escape" {
		m.dialog = nil
		return nil
	}

	switch d.kind {
	case dialogPalette:
		switch name {
		case "up", "ctrl+k":
			d.stepSelection(-1)
			return nil
		case "down", "ctrl+j":
			d.stepSelection(1)
			return nil
		case "pgup", "ctrl+u":
			d.pageSelection(-1)
			return nil
		case "pgdown", "ctrl+d":
			d.pageSelection(1)
			return nil
		case "enter", "return":
			if len(d.items) == 0 {
				return nil
			}
			action := d.items[d.selected].action
			m.dialog = nil
			return m.openAction(action)
		case "tab":
			return d.cycleField(false)
		case "shift+tab":
			return d.cycleField(true)
		}
		var cmd tea.Cmd
		d.fields[d.focus].input, cmd = d.fields[d.focus].input.Update(key)
		d.refreshPalette()
		return cmd

	case dialogEditSelect, dialogResultSort, dialogQueueSort:
		switch name {
		case "up", "ctrl+k", "k":
			d.stepSelection(-1)
			return nil
		case "down", "ctrl+j", "j":
			d.stepSelection(1)
			return nil
		case "pgup", "ctrl+u":
			d.pageSelection(-1)
			return nil
		case "pgdown", "ctrl+d":
			d.pageSelection(1)
			return nil
		case "enter", "return":
			if len(d.items) == 0 {
				return nil
			}
			if d.kind == dialogResultSort || d.kind == dialogQueueSort {
				action := d.items[d.selected].action
				m.dialog = nil
				if d.kind == dialogQueueSort {
					m.setQueueSort(queueSort(action))
				} else {
					m.setResultSort(resultSort(action))
				}
				return nil
			}
			field := client.CanonicalSetField(d.items[d.selected].action)
			value := ""
			if d.torrent != nil {
				value = setFieldValue(*d.torrent, field)
			}
			d.kind = dialogEditValue
			d.title = "Set " + field
			d.request.field = field
			d.fields = []dialogField{newDialogField("Value", value, "value", "qBittorrent validates this field")}
			return d.focusField(0)
		}
		return nil

	case dialogDetails, dialogHelp:
		innerWidth, _ := dialogDimensions(m.width)
		visible := max(1, m.heightForDialog()-9)
		count := len(dialogDetailLines(d.rows, innerWidth))
		switch name {
		case "up", "k":
			d.stepScroll(-1, visible, count)
		case "down", "j":
			d.stepScroll(1, visible, count)
		case "pgup", "ctrl+u":
			d.stepScroll(-visible, visible, count)
		case "pgdown", "ctrl+d", "space":
			d.stepScroll(visible, visible, count)
		case "home", "g":
			d.scroll = 0
		case "end", "G":
			d.scroll = max(0, count-visible)
		}
		return nil

	case dialogConfirm:
		switch name {
		case "y", "Y", "enter", "return":
			if m.busy != "" {
				d.status = "Finish the current request first"
				return nil
			}
			request := d.request
			m.dialog = nil
			return m.launchAction("remove", request)
		case "n", "N":
			m.dialog = nil
		}
		return nil

	case dialogDelete:
		if name == "enter" || name == "return" {
			if m.busy != "" {
				d.status = "Finish the current request first"
				return nil
			}
			if d.value(0) != "DELETE" {
				d.status = "Type DELETE exactly to confirm"
				return nil
			}
			request := d.request
			m.dialog = nil
			return m.launchAction("delete", request)
		}

	case dialogSearch, dialogFilter, dialogAdd, dialogAddResult, dialogEditValue, dialogMove:
		switch name {
		case "tab":
			return d.cycleField(false)
		case "shift+tab":
			return d.cycleField(true)
		case "enter", "return":
			return m.submitDialog()
		}
	}

	if len(d.fields) == 0 || d.focus < 0 || d.focus >= len(d.fields) {
		return nil
	}
	var cmd tea.Cmd
	d.fields[d.focus].input, cmd = d.fields[d.focus].input.Update(key)
	return cmd
}

func (m *model) submitDialog() tea.Cmd {
	d := m.dialog
	if d == nil {
		return nil
	}
	switch d.kind {
	case dialogSearch:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		query := strings.TrimSpace(d.value(0))
		plugin := strings.TrimSpace(d.value(1))
		timeout, err := time.ParseDuration(strings.TrimSpace(d.value(2)))
		if query == "" || plugin == "" {
			d.status = "Query and plugin are required"
			return nil
		}
		if err != nil || timeout <= 0 {
			d.status = "Timeout must be a positive duration such as 30s"
			return nil
		}
		m.dialog = nil
		return m.launchSearch(query, plugin, timeout)

	case dialogFilter:
		m.setFilter(strings.TrimSpace(d.value(0)), strings.TrimSpace(d.value(1)))
		m.dialog = nil
		return nil

	case dialogAdd:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		source := strings.TrimSpace(d.value(0))
		if source == "" {
			d.status = "A magnet, URL, or torrent file path is required"
			return nil
		}
		m.dialog = nil
		return m.launchAction("add", actionRequest{kind: "add", value: source})

	case dialogAddResult:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		id, err := strconv.ParseInt(strings.TrimSpace(d.value(0)), 10, 64)
		if err != nil || id <= 0 {
			d.status = "Enter a positive numeric result ID"
			return nil
		}
		m.dialog = nil
		return m.launchAction("add result", actionRequest{kind: "add-result", id: id})

	case dialogEditValue:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		if d.request.field == "" {
			d.status = "Choose a field first"
			return nil
		}
		m.dialog = nil
		return m.launchAction("edit", actionRequest{kind: "set", hashes: d.request.hashes, field: d.request.field, value: d.value(0)})

	case dialogMove:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		path := strings.TrimSpace(d.value(0))
		if path == "" {
			d.status = "A destination path is required"
			return nil
		}
		m.dialog = nil
		return m.launchAction("move", actionRequest{kind: "move", hashes: d.request.hashes, value: path})
	}
	return nil
}

func (m *model) heightForDialog() int {
	if m.height > 0 {
		return m.height
	}
	return 24
}

func dialogDimensions(width int) (innerWidth, padX int) {
	if width < 1 {
		width = 80
	}
	boxWidth := width - 4
	if boxWidth < 8 {
		boxWidth = width
	}
	if boxWidth > 94 {
		boxWidth = 94
	}
	padX = 2
	if boxWidth < 8 {
		padX = 0
	}
	return max(1, boxWidth-2*padX-2), padX
}

func dialogDetailLines(rows []detailRow, width int) []string {
	var lines []string
	for _, row := range rows {
		label := viewSafe(strings.TrimSpace(row.label))
		line := viewSafe(strings.TrimSpace(row.value))
		if label != "" {
			line = label + ": " + line
		}
		lines = append(lines, strings.Split(ansi.Wrap(line, width, "/"), "\n")...)
	}
	return lines
}

func (m *model) dialogView(width, height int) string {
	d := m.dialog
	if d == nil {
		return ""
	}
	if height < 1 {
		height = 24
	}
	innerWidth, padX := dialogDimensions(width)

	lines := make([]string, 0, 24)
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("37")).Render(clipDialog(viewSafe(d.title), innerWidth)))
	lines = append(lines, "")

	switch d.kind {
	case dialogPalette:
		if len(d.fields) > 0 {
			lines = append(lines, dialogInputLine(d.fields[0], d.focus == 0, innerWidth))
			lines = append(lines, "")
		}
		visible := max(3, height-10)
		start := d.selected - visible/2
		if start < 0 {
			start = 0
		}
		if start > len(d.items)-visible {
			start = max(0, len(d.items)-visible)
		}
		end := min(len(d.items), start+visible)
		for i := start; i < end; i++ {
			item := d.items[i]
			prefix := "  "
			if i == d.selected {
				prefix = "› "
			}
			label := prefix + item.label
			if item.keys != "" {
				label += "  " + item.keys
			}
			if item.description != "" {
				label += " — " + item.description
			}
			label = clipDialog(label, innerWidth)
			if i == d.selected {
				label = lipgloss.NewStyle().Foreground(lipgloss.Color("37")).Bold(true).Render(label)
			}
			lines = append(lines, label)
		}
		if len(d.items) == 0 {
			lines = append(lines, lipgloss.NewStyle().Faint(true).Render("  No matching commands"))
		}

	case dialogEditSelect, dialogResultSort, dialogQueueSort:
		visible := max(3, height-8)
		start := d.selected - visible/2
		if start < 0 {
			start = 0
		}
		if start > len(d.items)-visible {
			start = max(0, len(d.items)-visible)
		}
		end := min(len(d.items), start+visible)
		for i := start; i < end; i++ {
			prefix := "  "
			if i == d.selected {
				prefix = "› "
			}
			line := prefix + d.items[i].label
			if d.items[i].description != "" {
				line += " — " + d.items[i].description
			}
			line = clipDialog(line, innerWidth)
			if i == d.selected {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("37")).Bold(true).Render(line)
			}
			lines = append(lines, line)
		}

	case dialogDetails, dialogHelp:
		visible := max(1, height-9)
		details := dialogDetailLines(d.rows, innerWidth)
		start := min(d.scroll, max(0, len(details)-visible))
		end := min(len(details), start+visible)
		lines = append(lines, details[start:end]...)
		if len(details) == 0 {
			lines = append(lines, lipgloss.NewStyle().Faint(true).Render("(no details)"))
		}

	case dialogConfirm, dialogDelete:
		for _, line := range strings.Split(d.message, "\n") {
			lines = append(lines, clipDialog(viewSafe(line), innerWidth))
		}
		if len(d.fields) > 0 {
			lines = append(lines, "", dialogInputLine(d.fields[0], d.focus == 0, innerWidth))
		}

	default:
		for i := range d.fields {
			lines = append(lines, dialogInputLine(d.fields[i], i == d.focus, innerWidth))
			if d.fields[i].help != "" {
				lines = append(lines, lipgloss.NewStyle().Faint(true).Render("  "+clipDialog(d.fields[i].help, innerWidth-2)))
			}
		}
	}

	if d.status != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(clipDialog(viewSafe(d.status), innerWidth)))
	}

	footer := "Esc close"
	switch d.kind {
	case dialogPalette:
		footer = "↑/↓ choose · Enter select · type to filter · Esc close"
	case dialogEditSelect:
		footer = "↑/↓ choose · Enter edit · Esc close"
	case dialogResultSort, dialogQueueSort:
		footer = "↑/↓ choose · Enter apply · Esc close"
	case dialogDetails, dialogHelp:
		footer = "↑/↓ scroll · PgUp/PgDn page · Esc close"
	case dialogConfirm:
		footer = "y/Enter confirm · n/Esc cancel"
	case dialogDelete:
		footer = "type DELETE · Enter confirm · Esc cancel"
	default:
		footer = "Tab next · Shift+Tab previous · Enter submit · Esc cancel"
	}
	lines = append(lines, "", lipgloss.NewStyle().Faint(true).Render(clipDialog(footer, innerWidth)))

	// Keep the modal bounded even when a torrent name, tracker URL, or error
	// includes a very long line. The renderer can then center it predictably.
	maxLines := max(1, height-4)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	content := strings.Join(lines, "\n")
	style := lipgloss.NewStyle().
		Width(innerWidth).
		Padding(1, padX).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	return style.Render(content)
}

func dialogInputLine(field dialogField, focused bool, width int) string {
	label := strings.TrimSpace(field.label)
	if label == "" {
		label = "Value"
	}
	prefix := "  "
	if focused {
		prefix = "› "
	}
	available := max(1, width-lipgloss.Width(prefix+label)-3)
	field.input.SetWidth(available)
	value := field.input.View()
	line := prefix + label + ": " + value
	return clipDialog(line, width)
}

func clipDialog(value string, width int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == utf8.RuneError {
			if r == '\t' {
				return ' '
			}
			return ' '
		}
		return r
	}, value)
	if width < 1 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "…")
}

func setFieldValue(torrent client.TorrentInfo, field string) string {
	switch client.CanonicalSetField(field) {
	case "category":
		return torrent.Category
	case "tags":
		return torrent.Tags
	case "up-limit":
		return strconv.FormatInt(torrent.UpLimit, 10)
	case "dl-limit":
		return strconv.FormatInt(torrent.DLLimit, 10)
	case "ratio-limit":
		return strconv.FormatFloat(torrent.RatioLimit, 'f', -1, 64)
	case "seedtime-limit":
		return strconv.FormatInt(torrent.SeedingTimeLimit, 10)
	case "seqdl":
		return strconv.FormatBool(torrent.SequentialDL)
	case "autotmm":
		return strconv.FormatBool(torrent.AutoTMM)
	case "superseed":
		return strconv.FormatBool(torrent.SuperSeeding)
	default:
		return ""
	}
}

func joinHashes(hashes []string) string {
	return strings.Join(hashes, "|")
}
