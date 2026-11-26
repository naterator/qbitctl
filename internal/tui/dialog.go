//go:build !plan9

package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	"github.com/naterator/qbitctl/pkg/client"
)

// dialogKind identifies the small, modal interactions used by the TUI.  The
// dialog deliberately owns its widgets: the queue, search, and server pages
// remain useful while a dialog is open, but do not need to know how a form is
// laid out or how its focus moves.
type dialogKind string

const (
	dialogPalette             dialogKind = "palette"
	dialogSearch              dialogKind = "search"
	dialogFilter              dialogKind = "filter"
	dialogAdd                 dialogKind = "add"
	dialogAddResult           dialogKind = "add-result"
	dialogEditSelect          dialogKind = "edit-select"
	dialogShareChoice         dialogKind = "share-choice"
	dialogResultSort          dialogKind = "result-sort"
	dialogQueueSort           dialogKind = "queue-sort"
	dialogQueueColumns        dialogKind = "queue-columns"
	dialogRefreshInterval     dialogKind = "refresh-interval"
	dialogCopy                dialogKind = "copy"
	dialogEditValue           dialogKind = "edit-value"
	dialogMove                dialogKind = "move"
	dialogRename              dialogKind = "rename"
	dialogConfirm             dialogKind = "confirm"
	dialogDelete              dialogKind = "delete"
	dialogDetails             dialogKind = "details"
	dialogHelp                dialogKind = "help"
	dialogConfig              dialogKind = "config"
	dialogTheme               dialogKind = "theme"
	dialogSearchPlugins       dialogKind = "search-plugins"
	dialogSearchPluginManage  dialogKind = "search-plugin-manage"
	dialogSearchPluginActions dialogKind = "search-plugin-actions"
	dialogSearchPluginInstall dialogKind = "search-plugin-install"
	dialogProfilePicker       dialogKind = "profile-picker"
	dialogTrackerAdd          dialogKind = "tracker-add"
	dialogTrackerEdit         dialogKind = "tracker-edit"
	dialogTransferLimit       dialogKind = "transfer-limit"
)

type dialogField struct {
	label       string
	help        string
	placeholder string
	input       textinput.Model
}

type dialogItem struct {
	group       string
	label       string
	action      string
	description string
	keys        string
	disabled    bool
}

type dialog struct {
	kind  dialogKind
	title string

	fields []dialogField
	focus  int
	// advanced keeps the less common add-time controls collapsed until the
	// user asks for them. Values remain in fields while collapsed so toggling
	// the section never discards in-progress input.
	advanced bool

	// Palette dialogs keep the unfiltered list so typing can cheaply rebuild
	// the visible list without losing the action context.
	allItems []dialogItem
	items    []dialogItem
	selected int
	// paletteStatic is used by category/tag queue filter pickers. Those
	// palettes use the same keyboard and rendering path as the command palette,
	// but must not be replaced by the contextual command list after background
	// queue work completes.
	paletteStatic bool

	rows           []detailRow
	scroll         int
	torrentDetails *torrentDetailTarget
	trackerTitle   string
	trackerEntries []client.TrackerEntry
	trackerReveal  bool
	detail         *detailDialogState
	renamePath     *torrentPathRename

	request actionRequest
	// confirmAction overrides the legacy remove action when a confirmation is
	// used for another destructive mutation (for example share-limit action).
	confirmAction string
	// confirmParent returns to a live details dialog after a contextual
	// confirmation, such as banning the selected peer.
	confirmParent *dialog
	// torrents are captured when the edit dialog opens. Marked rows are edited
	// together, while an unmarked queue uses its focused row.
	torrents       []client.TorrentInfo
	mixed          bool
	status         string
	message        string
	pluginName     string
	copyValues     map[string]string
	profileStartup bool
}

func newDialogField(label, value, placeholder, help string) dialogField {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.SetStyles(viewInputStyle(viewFieldColor))
	input.SetValue(value)
	return dialogField{
		label:       label,
		help:        help,
		placeholder: placeholder,
		input:       input,
	}
}

func newPasswordDialogField(label, placeholder, help string) dialogField {
	field := newDialogField(label, "", placeholder, help)
	field.input.EchoMode = textinput.EchoPassword
	field.input.EchoCharacter = '•'
	return field
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
	if !d.fieldVisible(index) {
		for i := 0; i < len(d.fields); i++ {
			if d.fieldVisible(i) {
				index = i
				break
			}
		}
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
	delta := 1
	if backward {
		delta = -1
	}
	next := d.focus
	for i := 0; i < len(d.fields); i++ {
		next += delta
		if next < 0 {
			next = len(d.fields) - 1
		}
		if next >= len(d.fields) {
			next = 0
		}
		if d.fieldVisible(next) {
			return d.focusField(next)
		}
	}
	return nil
}

func (d *dialog) fieldVisible(index int) bool {
	if d == nil || index < 0 || index >= len(d.fields) {
		return false
	}
	if (d.kind == dialogAdd || d.kind == dialogAddResult) && !d.advanced {
		return index < addContentLayoutField
	}
	return true
}

func (d *dialog) stepSelection(delta int) {
	if d == nil {
		return
	}
	count := d.selectableCount()
	if count == 0 {
		return
	}
	d.scroll = 0
	d.selected += delta
	if d.selected < 0 {
		d.selected = count - 1
	}
	if d.selected >= count {
		d.selected = 0
	}
}

func (d *dialog) pageSelection(delta int) {
	if d == nil {
		return
	}
	d.selected = max(0, min(d.selected+delta*8, d.selectableCount()-1))
}

// Disabled providers are kept at the end of the picker for browsing.
func (d *dialog) selectableCount() int {
	count := len(d.items)
	for count > 0 && d.items[count-1].disabled {
		count--
	}
	return count
}

func (d *dialog) listStart(visible int) int {
	return min(max(d.scroll, d.selected-visible/2), max(0, len(d.items)-visible))
}

func (d *dialog) bodyHeight(height int) int {
	visible := dialogBodyHeight(height)
	if d.status != "" {
		visible = max(1, visible-1)
	}
	return visible
}

func (m *model) pageDialogItems(delta int) {
	d := m.dialog
	if d.kind == dialogSearchPlugins && (d.scroll > 0 || (delta > 0 && d.selected == d.selectableCount()-1)) {
		visible := d.bodyHeight(m.heightForDialog())
		// Page through a long disabled tail without moving the selection.
		centered := min(max(0, d.selected-visible/2), max(0, len(d.items)-visible))
		next := min(max(0, d.listStart(visible)+delta*max(1, visible-1)), max(0, len(d.items)-visible))
		d.scroll = 0
		if next > centered {
			d.scroll = next
		}
		return
	}
	d.pageSelection(delta)
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
	rawAction := strings.TrimSpace(action)
	if strings.HasPrefix(rawAction, queueFilterCategoryAction) {
		m.dialog = nil
		m.setCategoryFilter(strings.TrimPrefix(rawAction, queueFilterCategoryAction))
		return nil
	}
	if strings.HasPrefix(rawAction, queueFilterTagAction) {
		m.dialog = nil
		m.setTagFilter(strings.TrimPrefix(rawAction, queueFilterTagAction))
		return nil
	}
	switch strings.ToLower(rawAction) {
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
	case "config", "config-page", "settings":
		m.dialog = nil
		return m.goTo(configPage)
	case "profile", "profiles", "switch-profile":
		return m.openProfilePicker()
	case "startup-profile", "startup-profile-picker":
		return m.openStartupProfilePicker()
	case "previous-item":
		return m.handleKey("up")
	case "next-item":
		return m.handleKey("down")
	case "previous-page":
		return m.handleKey("pgup")
	case "next-page":
		return m.handleKey("pgdown")
	case "first-item":
		return m.handleKey("home")
	case "last-item":
		return m.handleKey("end")
	case "search":
		return m.openSearchDialog()
	case "cancel-search":
		if m.busy != "" && m.work == searchWork && m.cancel != nil {
			m.cancelSearch()
		}
		return nil
	case "cancel-plugin-request":
		if m.busy != "" && m.work == searchPluginManageWork && m.cancel != nil {
			m.cancelSearchPluginAction()
		}
		return nil
	case "cancel-queue-filter":
		if m.busy != "" && m.work == queueCatalogWork && m.cancel != nil {
			m.cancel()
			m.busy = "Canceling filter catalog"
		}
		return nil
	case "cancel-connection":
		if m.busy != "" && m.work == connectWork && m.cancel != nil {
			m.connectCanceled = true
			m.cancel()
			m.busy = "Canceling connection"
		}
		return nil
	case "providers", "search-providers", "search plugins":
		if m.page != searchPage {
			return nil
		}
		return m.openSearchPluginPicker()
	case "filter":
		m.openFilterDialog()
		return nil
	case "filter-category", "category-filter":
		return m.openQueueCatalogAction("category")
	case "filter-tag", "tag-filter":
		return m.openQueueCatalogAction("tag")
	case "clear-queue-filter":
		if m.page == queuePage {
			m.setQueueFilter(queueFilter{state: "all"})
		}
		return nil
	case "clear-result-filter":
		if m.page == searchPage {
			m.clearResultFilter()
		}
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
	case "queue-columns", "columns":
		if m.page == queuePage {
			m.openQueueColumnsDialog()
		}
		return nil
	case "details":
		m.dialog = nil
		return m.loadDetails(false)
	case "copy":
		return m.openCopyDialog()
	case "server-download-limit", "transfer-download-limit":
		if m.page == serverPage {
			return m.openTransferLimitDialog(false)
		}
		return nil
	case "server-upload-limit", "transfer-upload-limit":
		if m.page == serverPage {
			return m.openTransferLimitDialog(true)
		}
		return nil
	case "server-mode-normal", "transfer-mode-normal", "server-mode-alternate", "transfer-mode-alternate":
		if m.page != serverPage || m.busy != "" {
			return nil
		}
		mode := "normal"
		if strings.HasSuffix(strings.ToLower(action), "alternate") {
			mode = "alternate"
		}
		m.dialog = nil
		return m.launchAction("speed limits mode", actionRequest{kind: "transfer-mode", value: mode})
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
	case "start", "pause", "force-start", "recheck", "reannounce", "queue-up", "queue-down", "queue-top", "queue-bottom":
		hashes := m.targetHashes()
		if len(hashes) == 0 {
			m.setNotice("Select or mark a torrent first", false)
			return nil
		}
		if m.busy != "" {
			m.setNotice("Finish the current request first", false)
			return nil
		}
		m.dialog = nil
		return m.launchAction(action, actionRequest{kind: action, hashes: joinHashes(hashes)})
	case "rename":
		hashes := m.targetHashes()
		if len(hashes) == 0 {
			m.setNotice("Select or mark a torrent first", false)
			return nil
		}
		if len(hashes) > 1 {
			m.setNotice("Rename one torrent at a time", false)
			return nil
		}
		if m.busy != "" {
			m.setNotice("Finish the current request first", false)
			return nil
		}
		m.openRenameDialog()
		return nil
	case "move":
		if len(m.targetHashes()) == 0 {
			m.setNotice("Select or mark a torrent first", false)
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
	case "toggle-refresh":
		m.dialog = nil
		m.refreshPaused = !m.refreshPaused
		if !m.refreshPaused && (m.page == queuePage || m.page == serverPage) {
			return m.refresh()
		}
		return nil
	case "toggle-mark":
		if m.page == queuePage {
			m.toggleMark()
		}
		return nil
	case "clear-marks", "clear marks":
		m.clearMarks()
		m.dialog = nil
		return nil
	case "help":
		m.openHelpDialog()
		return nil
	case "config-server":
		return m.openConfigSetting(configSettingServer)
	case "config-username":
		return m.openConfigSetting(configSettingUsername)
	case "config-password":
		return m.openConfigSetting(configSettingPassword)
	case "config-ca":
		return m.openConfigSetting(configSettingCAFile)
	case "config-default":
		return m.openConfigSetting(configSettingTUIDefault)
	case "config-theme":
		return m.openConfigSetting(configSettingTheme)
	case "config-units":
		return m.openConfigSetting(configSettingDataUnits)
	case "config-refresh":
		return m.openConfigSetting(configSettingRefreshInterval)
	case "config-provider":
		return m.openConfigSetting(configSettingSearchProvider)
	case "config-file":
		return m.openConfigSetting(configSettingConfigFile)
	case "config-cache":
		return m.openConfigSetting(configSettingSearchCache)
	case "quit", "exit":
		m.dialog = nil
		return m.requestQuit()
	default:
		return nil
	}
}

func (m *model) openSearchDialog() tea.Cmd {
	plugin := strings.TrimSpace(m.searchPlugin)
	if plugin == "" {
		plugin = defaultSearchPluginSelection
	}
	timeout := m.searchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	d := &dialog{
		kind:  dialogSearch,
		title: "Search " + m.searchPluginLabel(plugin),
		fields: []dialogField{
			newDialogField("Query", m.query, "Ubuntu 24.04", "The torrent search phrase"),
			newDialogField("Plugin", viewSafe(plugin), defaultSearchPluginSelection, m.searchPluginHelp()),
			newDialogField("Timeout", timeout.String(), "30s", "Go duration, for example 45s or 2m"),
		},
	}
	m.dialog = d
	d.focusField(0)
	return m.ensureSearchPlugins()
}

// openSearchPluginPicker presents the provider catalog without starting a
// search. The selected value is applied to the next search and the current
// result set remains untouched.
func (m *model) openSearchPluginPicker() tea.Cmd {
	m.dialog = &dialog{
		kind:  dialogSearchPlugins,
		title: "Choose search provider",
	}
	m.refreshSearchPluginPicker()
	// Keep the current catalog visible while refreshing it. This also makes
	// reopening the picker useful when another qBittorrent client changed the
	// installed plugins after the first Search-page visit.
	return m.ensureSearchPlugins(true)
}

func (m *model) searchPluginPickerItems() []dialogItem {
	items := []dialogItem{
		{label: "Enabled search plugins", action: defaultSearchPluginSelection, description: "server-enabled providers"},
		{label: "All search plugins", action: "all", description: "every installed provider"},
	}
	plugins := append([]searchPluginInfo(nil), m.searchPlugins...)
	sort.SliceStable(plugins, func(i, j int) bool {
		if plugins[i].Enabled != plugins[j].Enabled {
			return plugins[i].Enabled
		}
		left := strings.ToLower(searchPluginFullName(plugins[i]))
		right := strings.ToLower(searchPluginFullName(plugins[j]))
		if left == right {
			return strings.ToLower(plugins[i].Name) < strings.ToLower(plugins[j].Name)
		}
		return left < right
	})
	for _, plugin := range plugins {
		name := strings.TrimSpace(plugin.Name)
		if name == "" {
			continue
		}
		description := viewSafe(name)
		if plugin.Enabled {
			description += " · enabled"
		} else {
			description += " · disabled"
		}
		items = append(items, dialogItem{
			label:       searchPluginFullName(plugin),
			action:      name,
			description: description,
			disabled:    !plugin.Enabled,
		})
	}
	return items
}

func (m *model) refreshSearchPluginPicker() {
	if m.dialog == nil || m.dialog.kind != dialogSearchPlugins {
		return
	}
	selectedAction := ""
	if m.dialog.selected >= 0 && m.dialog.selected < len(m.dialog.items) {
		selectedAction = m.dialog.items[m.dialog.selected].action
	}
	if selectedAction == "" {
		current := m.searchPlugin
		if m.page == configPage {
			current = m.savedSearchPlugin
		}
		selectedAction = m.searchPluginSelection(current)
	}
	m.dialog.items = m.searchPluginPickerItems()
	m.dialog.selected = 0
	m.dialog.scroll = 0
	for i, item := range m.dialog.items[:m.dialog.selectableCount()] {
		if item.action == selectedAction {
			m.dialog.selected = i
			break
		}
	}
}

func searchPluginLabel(plugin string) string {
	plugin = strings.TrimSpace(plugin)
	if plugin == "" {
		return "search plugin"
	}
	runes := []rune(plugin)
	return viewSafe(strings.ToUpper(string(runes[0])) + string(runes[1:]))
}

func (m *model) searchPluginLabel(plugin string) string {
	selection := m.searchPluginSelection(plugin)
	if selection == "" || selection == defaultSearchPluginSelection {
		return "Enabled search plugins"
	}
	if strings.EqualFold(selection, "all") {
		return "All search plugins"
	}
	parts := strings.Split(selection, "|")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		labels = append(labels, m.searchPluginName(part))
	}
	if len(labels) == 0 {
		return "Enabled search plugins"
	}
	return strings.Join(labels, " | ")
}

func (m *model) searchPluginName(name string) string {
	for _, plugin := range m.searchPlugins {
		if plugin.Name == name {
			return searchPluginFullName(plugin)
		}
	}
	for _, plugin := range m.searchPlugins {
		if strings.EqualFold(plugin.Name, name) {
			return searchPluginFullName(plugin)
		}
	}
	return searchPluginLabel(name)
}

func searchPluginFullName(plugin searchPluginInfo) string {
	if full := strings.TrimSpace(plugin.FullName); full != "" {
		return viewSafe(full)
	}
	return searchPluginLabel(plugin.Name)
}

func (m *model) searchPluginSelection(value string) string {
	selection := strings.TrimSpace(value)
	if resolved, err := client.ResolveSearchPluginSelection(selection, m.searchPlugins); err == nil {
		if resolved == "multi" {
			return defaultSearchPluginSelection
		}
		return resolved
	}
	// The backend can resolve this against a newer catalog or report an
	// invalid selector. Keep the user's input intact until then.
	return selection
}

func (m *model) searchPluginHelp() string {
	const choices = "Use enabled, all, a plugin name, or plugin|plugin"
	if m.searchPluginsLoading {
		return "Loading installed plugins… " + choices
	}
	if !m.searchPluginsAttempted && m.searchPluginsError == "" {
		return "Installed plugin list pending. " + choices
	}
	if len(m.searchPlugins) == 0 {
		if m.searchPluginsError != "" {
			return "Installed plugin list unavailable. " + choices
		}
		return "No installed plugins reported. " + choices
	}
	names := make([]string, 0, len(m.searchPlugins))
	for _, plugin := range m.searchPlugins {
		if strings.TrimSpace(plugin.Name) == "" {
			continue
		}
		name := searchPluginFullName(plugin)
		if !strings.EqualFold(name, plugin.Name) {
			name += " (" + viewSafe(plugin.Name) + ")"
		}
		if plugin.Enabled {
			name += " ✓"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "No installed plugins reported. " + choices
	}
	prefix := "Installed: "
	if m.searchPluginsStale {
		prefix = "Installed (stale): "
	}
	return prefix + strings.Join(names, ", ") + ". " + choices
}

func (m *model) refreshSearchDialogPlugin() {
	if m.dialog == nil || m.dialog.kind != dialogSearch || len(m.dialog.fields) < 2 {
		return
	}
	selection := m.searchPluginSelection(m.dialog.value(1))
	if selection == "" {
		selection = defaultSearchPluginSelection
	}
	m.dialog.title = "Search " + m.searchPluginLabel(selection)
	m.dialog.fields[1].help = m.searchPluginHelp()
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
		fields: torrentAddFields(
			newDialogField("Source", "", "magnet, URL, or .torrent path", "Local torrent files are uploaded to qBittorrent"),
		),
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
		fields: torrentAddFields(
			newDialogField("Result ID", value, "numeric ID from the search page", "Use a recent result ID from this server and user"),
		),
	}
	m.dialog = d
	d.focusField(0)
}

func torrentAddFields(source dialogField) []dialogField {
	return []dialogField{
		source,
		newDialogField("Category", "", "optional category", "Category assigned by qBittorrent"),
		newDialogField("Destination", "", "optional qBittorrent path", "Save path on the qBittorrent host"),
		newDialogField("Stopped", "no", "yes or no", "Add the torrent stopped instead of starting it"),
		newDialogField("Tags", "", "optional comma-separated tags", "Tags assigned by qBittorrent"),
		newDialogField("Advanced: Content layout", "", "default, original, subfolder, no-subfolder", "Optional layout for files inside the download"),
		newDialogField("Advanced: Download path", "", "optional qBittorrent path", "Separate incomplete/download path on the qBittorrent host"),
		newDialogField("Advanced: Skip checking", "default", "default, yes, no", "Skip hash checking and start seeding"),
		newDialogField("Advanced: Stop condition", "default", "default, none, metadata, files", "Stop after metadata or files are checked"),
		newDialogField("Advanced: Sequential download", "default", "default, yes, no", "Download pieces in sequential order"),
		newDialogField("Advanced: First/last priority", "default", "default, yes, no", "Prioritize the first and last pieces"),
		newDialogField("Advanced: Share ratio", "", "-2 global, -1 unlimited, or non-negative", "Share ratio limit applied at add time"),
		newDialogField("Advanced: Seeding time", "", "duration, -1, or -2", "Share seeding limit; durations such as 3d or 90m"),
		newDialogField("Advanced: Inactive seeding", "", "duration, -1, or -2", "Inactive share seeding limit"),
		newDialogField("Advanced: Share limits mode", "default", "default, match-any, match-all", "How multiple share limits are combined"),
		newDialogField("Advanced: Share limit action", "default", "default, stop, remove, remove-with-content, enable-super-seeding", "Action when share limits are reached"),
		newDialogField("Confirm destructive action", "no", "yes or no", "Required when share limit action removes a torrent"),
	}
}

func (m *model) openEditDialog() {
	hashes := m.targetHashes()
	if len(hashes) == 0 {
		m.setNotice("Select or mark a torrent first", false)
		return
	}
	byHash := make(map[string]client.TorrentInfo, len(m.allTorrents))
	for _, torrent := range m.allTorrents {
		byHash[torrent.Hash] = torrent
	}
	for _, torrent := range m.queue {
		byHash[torrent.Hash] = torrent
	}
	torrents := make([]client.TorrentInfo, 0, len(hashes))
	for _, hash := range hashes {
		if torrent, ok := byHash[hash]; ok {
			torrents = append(torrents, torrent)
		}
	}
	if len(torrents) == 0 {
		m.setNotice("Select or mark a torrent first", false)
		return
	}
	items := make([]dialogItem, 0, len(client.SetFieldNames))
	for _, field := range client.SetFieldNames {
		items = append(items, dialogItem{label: field, action: field, description: "Set this qBittorrent field"})
	}
	title := "Edit torrent field"
	if len(torrents) > 1 {
		title = fmt.Sprintf("Edit %d torrents", len(torrents))
	}
	d := &dialog{
		kind:     dialogEditSelect,
		title:    title,
		allItems: items,
		items:    append([]dialogItem(nil), items...),
		torrents: torrents,
		request:  actionRequest{hashes: joinHashes(hashes)},
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
		m.setNotice("Select or mark a torrent first", false)
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

func (m *model) openRenameDialog() {
	hashes := m.targetHashes()
	if len(hashes) == 0 {
		m.setNotice("Select or mark a torrent first", false)
		return
	}
	if len(hashes) > 1 {
		m.setNotice("Rename one torrent at a time", false)
		return
	}
	if m.busy != "" {
		m.setNotice("Finish the current request first", false)
		return
	}
	name := ""
	for _, torrent := range m.queue {
		if strings.EqualFold(torrent.Hash, hashes[0]) {
			name = torrent.Name
			break
		}
	}
	d := &dialog{
		kind:    dialogRename,
		title:   "Rename torrent",
		request: actionRequest{kind: "rename", hashes: hashes[0]},
		fields: []dialogField{
			newDialogField("Name", name, "New torrent name", "The name shown by qBittorrent"),
		},
	}
	m.dialog = d
	d.focusField(0)
}

func (m *model) openRemoveDialog(deleteFiles bool) tea.Cmd {
	hashes := m.targetHashes()
	if len(hashes) == 0 {
		m.setNotice("Select or mark a torrent first", false)
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
		suffix = "Permanently deletes files. Type DELETE to continue."
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
	m.dialog = &dialog{kind: dialogHelp}
}

func (m *model) openConfigDialog() {
	m.openConnectionEditor(true)
}

func (m *model) openConnectionEditor(includeStartup bool) {
	// ReadSetup exposes only HasPassword metadata. Keep this field empty even
	// when a password is saved so the secret can never be rendered or copied
	// into the TUI.
	passwordPlaceholder := "Leave blank to keep saved password"
	passwordHelp := "Typed characters are hidden; blank keeps the saved password"
	if !m.setup.hasPassword {
		passwordPlaceholder = "Password required for first connection"
		passwordHelp = "Typed characters are hidden; a password is required"
	}
	d := &dialog{
		kind:  dialogConfig,
		title: "Configure qBittorrent",
		fields: []dialogField{
			newDialogField("Server", m.setup.url, "http://localhost:8080", "qBittorrent Web API address"),
			newDialogField("Username", m.setup.user, "admin", "Web API username"),
			newPasswordDialogField("Password", passwordPlaceholder, passwordHelp),
		},
	}
	if includeStartup {
		defaultChoice := "no"
		if m.setup.tuiDefault {
			defaultChoice = "yes"
		}
		d.fields = append(d.fields, newDialogField("TUI default", defaultChoice, "yes or no", "Start qbitctl in the TUI by default"))
	}
	d.fields = append(d.fields, newDialogField("CA bundle", m.setup.caFile, "System trust store", "Optional PEM CA bundle path; blank keeps the saved setting"))
	m.dialog = d
	d.focusField(0)
}

func (m *model) openDetailDialog(title string, rows []detailRow) {
	copyRows := append([]detailRow(nil), rows...)
	m.dialog = &dialog{kind: dialogDetails, title: title, rows: copyRows, detail: newDetailDialogState()}
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
	if name == "ctrl+p" && m.busy != "" && m.work == searchPluginManageWork {
		m.openPaletteDialog()
		return nil
	}
	if (d.kind == dialogAdd || d.kind == dialogAddResult) && name == "f2" {
		d.advanced = !d.advanced
		if !d.advanced && !d.fieldVisible(d.focus) {
			d.focusField(addTagsField)
		}
		return nil
	}

	// Ctrl+C is intentionally handled by the parent model. This keeps one
	// cancellation policy for in-flight requests and for the interactive UI.
	if name == "esc" || name == "escape" || (name == "?" && d.kind == dialogHelp) ||
		(name == "ctrl+p" && d.kind == dialogPalette) {
		if m.busy != "" && m.work == searchPluginManageWork && m.cancel != nil {
			m.cancelSearchPluginAction()
			return nil
		}
		if d.kind == dialogTrackerAdd || d.kind == dialogTrackerEdit {
			m.closeTrackerForm()
			return nil
		}
		if d.kind == dialogDetails && d.detail != nil && d.detail.fileFilterActive {
			d.detail.fileFilterActive = false
			return nil
		}
		if d.renamePath != nil {
			m.dialog = d.renamePath.parent
			return nil
		}
		if d.confirmParent != nil {
			m.dialog = d.confirmParent
			return nil
		}
		if d.kind == dialogSearchPlugins && m.searchPluginsLoading {
			m.cancelSearchPlugins()
		}
		if d.kind == dialogTheme {
			m.themePreview = ""
		}
		m.dialog = nil
		return nil
	}
	if d.kind == dialogTrackerAdd || d.kind == dialogTrackerEdit {
		return m.updateTrackerForm(name, key)
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

	case dialogEditSelect, dialogShareChoice, dialogResultSort, dialogQueueSort, dialogQueueColumns, dialogCopy, dialogTheme, dialogSearchPlugins, dialogSearchPluginManage, dialogSearchPluginActions, dialogProfilePicker:
		if d.kind == dialogSearchPlugins && (name == "m" || name == "M") {
			return m.openSearchPluginManagement()
		}
		switch name {
		case "up", "ctrl+k", "k":
			d.stepSelection(-1)
			if d.kind == dialogTheme {
				m.previewThemeFromDialog()
			}
			return nil
		case "down", "ctrl+j", "j":
			d.stepSelection(1)
			if d.kind == dialogTheme {
				m.previewThemeFromDialog()
			}
			return nil
		case "pgup", "ctrl+u":
			m.pageDialogItems(-1)
			if d.kind == dialogTheme {
				m.previewThemeFromDialog()
			}
			return nil
		case "pgdown", "ctrl+d":
			m.pageDialogItems(1)
			if d.kind == dialogTheme {
				m.previewThemeFromDialog()
			}
			return nil
		case "enter", "return":
			if len(d.items) == 0 {
				return nil
			}
			if d.kind == dialogShareChoice && m.busy != "" {
				d.status = "Finish the current request first"
				return nil
			}
			if d.kind == dialogCopy {
				value := d.copyValues[d.items[d.selected].action]
				m.dialog = nil
				m.copyToTerminal(value)
				return nil
			}
			if d.kind == dialogTheme {
				id := terminaltheme.ID(d.items[d.selected].action)
				if !terminaltheme.Known(id) {
					m.themePreview = ""
					m.dialog = nil
					return nil
				}
				m.themePreview = ""
				m.dialog = nil
				if err := m.persistTheme(id); err != nil {
					m.setNotice("Color scheme: "+err.Error(), true)
					return nil
				}
				m.theme = id
				return nil
			}
			if d.kind == dialogResultSort || d.kind == dialogQueueSort || d.kind == dialogQueueColumns {
				action := d.items[d.selected].action
				m.dialog = nil
				if d.kind == dialogQueueSort {
					m.setQueueSort(queueSort(action))
				} else if d.kind == dialogQueueColumns {
					m.setQueueColumns(queueColumnPreset(action))
				} else {
					m.setResultSort(resultSort(action))
				}
				return nil
			}
			if d.kind == dialogSearchPlugins {
				if d.items[d.selected].disabled || d.selected < d.listStart(d.bodyHeight(m.heightForDialog())) {
					return nil
				}
				m.dialog = nil
				m.setDefaultSearchPlugin(d.items[d.selected].action)
				return nil
			}
			if d.kind == dialogProfilePicker {
				name := d.items[d.selected].action
				m.dialog = nil
				if d.profileStartup {
					if err := m.setStartupProfile(name); err != nil {
						m.setNotice("Startup profile: "+err.Error(), true)
					}
					return nil
				}
				return m.switchProfile(name)
			}
			if d.kind == dialogSearchPluginManage {
				return m.submitSearchPluginManagementSelection()
			}
			if d.kind == dialogSearchPluginActions {
				return m.submitSearchPluginAction()
			}
			if d.kind == dialogShareChoice {
				request := d.request
				request.value = d.items[d.selected].action
				request.kind = "set"
				m.dialog = nil
				if request.field == "share-limit-action" && client.IsDestructiveShareLimitAction(request.value) {
					m.dialog = &dialog{
						kind:          dialogConfirm,
						title:         "Confirm share-limit action",
						message:       shareLimitChoiceConfirmation(m, request.hashes, request.value),
						request:       request,
						confirmAction: "edit",
					}
					return nil
				}
				return m.launchAction("edit", request)
			}
			field := client.CanonicalSetField(d.items[d.selected].action)
			value := ""
			mixed := false
			if len(d.torrents) > 0 {
				value = setFieldValue(d.torrents[0], field)
				for _, torrent := range d.torrents[1:] {
					if setFieldValue(torrent, field) != value {
						mixed = true
						value = ""
						break
					}
				}
			}
			d.kind = dialogEditValue
			d.mixed = mixed
			d.title = "Set " + field
			if len(d.torrents) > 1 {
				d.title = fmt.Sprintf("Set %s for %d torrents", field, len(d.torrents))
			}
			d.request.field = field
			if field == "share-limits-mode" || field == "share-limit-action" {
				d.kind = dialogShareChoice
				d.title = "Set " + field
				d.items = shareLimitChoiceItems(field)
				d.selected = 0
				if !mixed {
					for i, item := range d.items {
						if item.action == value {
							d.selected = i
							break
						}
					}
				}
				return nil
			}
			placeholder := "value"
			help := "qBittorrent validates this field"
			if mixed {
				placeholder = "(mixed)"
				help = fmt.Sprintf("Applies to %d torrents", len(d.torrents))
			} else if len(d.torrents) > 1 {
				help = fmt.Sprintf("Applies to %d torrents", len(d.torrents))
			}
			d.fields = []dialogField{newDialogField("Value", value, placeholder, help)}
			return d.focusField(0)
		}
		return nil

	case dialogDetails, dialogHelp:
		innerWidth, _ := dialogDimensions(m.width)
		visible := dialogBodyHeight(m.heightForDialog())
		count := len(dialogDetailLines(d.rows, innerWidth))
		if d.kind == dialogDetails && d.torrentDetails != nil {
			if handled, cmd := m.updateTorrentDetailKey(d, name); handled {
				return cmd
			}
			visible = detailBodyHeight(d, m.heightForDialog())
			count = len(m.detailBodyLines(d, innerWidth))
		}
		if d.kind == dialogHelp {
			visible, count = helpViewport(viewContentWidth(m.width), m.heightForDialog()-viewHeaderLines-viewFooterLines)
		}
		d.scroll = min(d.scroll, max(0, count-visible))
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
		case "end", "G", "shift+g":
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
			m.dialog = d.confirmParent
			action := d.confirmAction
			if action == "" {
				action = "remove"
			}
			return m.launchAction(action, request)
		case "n", "N":
			m.dialog = d.confirmParent
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

	case dialogSearch, dialogFilter, dialogAdd, dialogAddResult, dialogEditValue, dialogMove, dialogRename, dialogConfig, dialogSearchPluginInstall, dialogRefreshInterval, dialogTransferLimit:
		switch name {
		case "tab":
			return d.cycleField(false)
		case "shift+tab":
			return d.cycleField(true)
		case "enter", "return":
			if d.kind == dialogSearchPluginInstall {
				return m.submitSearchPluginInstall(d.value(0))
			}
			if d.renamePath != nil {
				return m.submitTorrentPathRename(d)
			}
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
		plugin := m.searchPluginSelection(d.value(1))
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

	case dialogAdd, dialogAddResult:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		source := strings.TrimSpace(d.value(0))
		request := actionRequest{kind: "add", value: source}
		label := "add"
		if d.kind == dialogAddResult {
			id, err := strconv.ParseInt(source, 10, 64)
			if err != nil || id <= 0 {
				d.status = "Enter a positive numeric result ID"
				return nil
			}
			request = actionRequest{kind: "add-result", id: id}
			label = "add result"
		} else if source == "" {
			d.status = "A magnet, URL, or torrent file path is required"
			return nil
		}
		options, err := parseAddDialogOptions(d)
		if err != nil {
			d.status = err.Error()
			return nil
		}
		request.addOptions = options
		m.dialog = nil
		return m.launchAction(label, request)

	case dialogEditValue:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		if d.request.field == "" {
			d.status = "Choose a field first"
			return nil
		}
		if d.mixed && strings.TrimSpace(d.value(0)) == "" {
			d.status = fmt.Sprintf("Enter a value to apply to %d torrents", len(d.torrents))
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

	case dialogRename:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		name := strings.TrimSpace(d.value(0))
		if name == "" {
			d.status = "A torrent name is required"
			return nil
		}
		m.dialog = nil
		return m.launchAction("rename", actionRequest{kind: "rename", hashes: d.request.hashes, value: name})

	case dialogTransferLimit:
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		value := strings.TrimSpace(d.value(0))
		if value == "" {
			d.status = "Enter a limit or unlimited"
			return nil
		}
		request := d.request
		request.value = value
		m.dialog = nil
		return m.launchAction("transfer limit", request)

	case dialogConfig:
		choice := "no"
		caIndex := 3
		if len(d.fields) > 4 {
			choice = d.value(3)
			caIndex = 4
		} else if m.setup.tuiDefault {
			choice = "yes"
		}
		tuiDefault, err := parseTUIChoice(choice)
		if err != nil {
			d.status = err.Error()
			return nil
		}
		return m.saveConfig(client.Credentials{
			URL:  d.value(0),
			User: d.value(1),
			Pass: d.value(2),
		}, d.value(caIndex), tuiDefault)

	case dialogRefreshInterval:
		interval, err := parseRefreshInterval(d.value(0))
		if err != nil {
			d.status = err.Error()
			return nil
		}
		m.dialog = nil
		m.setRefreshInterval(interval)
		return nil
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
	if width < 12 {
		return width, 0
	}
	return min(76, width-8), 2
}

func dialogBodyHeight(height int) int {
	if height >= 11 {
		return height - 8
	}
	return max(1, height-2)
}

func dialogDetailLines(rows []detailRow, width int) []string {
	const columnGap = 4 // one cell of padding on either side of the label, then "│ "
	stacked := width < 32
	labelWidth, valueWidth := width, width
	indent := ""
	if !stacked {
		labelWidth = 0
		for _, row := range rows {
			labelWidth = max(labelWidth, lipgloss.Width(strings.TrimSpace(viewSafe(row.label))))
		}
		// Keep the value column wide when the server returns extra fields.
		labelWidth = min(labelWidth, width/3)
		valueWidth = width - labelWidth - columnGap
	} else if width >= 8 {
		indent = "  "
		valueWidth -= len(indent)
	}
	labelStyle := viewMutedStyle.Background(viewFieldColor)
	divider := viewRuleStyle.Render("│") + " "
	var lines []string
	for _, row := range rows {
		label := strings.TrimSpace(viewSafe(row.label))
		value := strings.TrimSpace(viewSafe(row.value))
		valueStyle := viewTextStyle
		if value == "" {
			value = "—"
			valueStyle = viewMutedStyle
		}
		labels := strings.Split(ansi.Wrap(label, labelWidth, ""), "\n")
		values := strings.Split(ansi.Wrap(value, valueWidth, "/"), "\n")
		if stacked {
			for _, part := range labels {
				lines = append(lines, viewMutedStyle.Render(part))
			}
			for _, part := range values {
				lines = append(lines, indent+valueStyle.Render(part))
			}
			continue
		}
		for i := range max(len(labels), len(values)) {
			labelPart, valuePart := "", ""
			if i < len(labels) {
				labelPart = strings.TrimSpace(labels[i])
			}
			if i < len(values) {
				valuePart = values[i]
			}
			inset := strings.Repeat(" ", max(0, labelWidth-lipgloss.Width(labelPart)))
			lines = append(lines, labelStyle.Render(" "+inset+labelPart+" ")+divider+valueStyle.Render(valuePart))
		}
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
	if d.kind == dialogPalette {
		width = min(66, width)
	}
	innerWidth, padX := dialogDimensions(width)
	visible := d.bodyHeight(height)
	var body []string
	footer := viewKeys(innerWidth, "tab", "next", "enter", "submit", "esc", "cancel")

	switch d.kind {
	case dialogPalette:
		body = m.dialogPaletteLines(innerWidth, visible)
		footer = viewKeys(innerWidth, "↑/↓", "move", "enter", "run")

	case dialogEditSelect, dialogShareChoice, dialogResultSort, dialogQueueSort, dialogQueueColumns, dialogCopy, dialogTheme, dialogSearchPlugins, dialogSearchPluginManage, dialogSearchPluginActions, dialogProfilePicker:
		start := d.listStart(visible)
		for i := start; i < min(len(d.items), start+visible); i++ {
			item := d.items[i]
			line := "  " + item.label
			if i == d.selected && !item.disabled {
				line = "› " + item.label
			}
			if item.description != "" {
				line += " · " + item.description
			}
			line = viewFit(line, innerWidth)
			if item.disabled {
				line = viewMutedStyle.Faint(true).Render(line)
			} else if i == d.selected {
				line = viewSelectedSty.Width(innerWidth).Render(line)
			}
			body = append(body, line)
		}
		footer = viewKeys(innerWidth, "↑/↓", "move", "enter", "select", "esc", "close")
		if d.kind == dialogTheme {
			footer = viewKeys(innerWidth, "↑/↓", "preview", "enter", "save", "esc", "cancel")
		}
		if d.kind == dialogSearchPlugins && len(d.items) > visible {
			footer = viewKeys(innerWidth, "↑/↓", "move", "pgup/pgdn", "browse", "enter", "select", "esc", "close")
		}

	case dialogDetails:
		var details []string
		footer = viewKeys(innerWidth, "↑/↓", "scroll", "esc", "close")
		if d.torrentDetails == nil {
			details = dialogDetailLines(d.rows, innerWidth)
		} else {
			state := m.detailState(d)
			body = append(body, detailTabHeader(state.tab, innerWidth), "")
			visible = detailBodyHeight(d, height)
			if state.tab == detailTabTrackers {
				body = append(body, trackerTabLines(d, innerWidth, visible)...)
				footer = viewKeys(innerWidth, "↑/↓", "select", "v", "full URL", "a", "add", "e", "edit", "d", "remove", "tab", "tabs", "esc", "close")
				break
			}
			details = m.detailBodyLines(d, innerWidth)
			footer = viewKeys(innerWidth, "1/2/3/4", "tabs", "↑/↓", "scroll", "esc", "close")
			switch state.tab {
			case detailTabFiles:
				footer = viewKeys(innerWidth, "space", "mark", "/", "filter", "0/n/6/7", "priority", "f2/f3", "rename", "tab", "tabs", "esc", "close")
			case detailTabPeers:
				footer = viewKeys(innerWidth, "b", "ban peer", "tab", "tabs", "esc", "close")
			}
		}
		start := min(d.scroll, max(0, len(details)-visible))
		body = append(body, details[start:min(len(details), start+visible)]...)
		if len(details) == 0 {
			body = append(body, viewMutedStyle.Render("No details available"))
		}

	case dialogConfirm, dialogDelete:
		message := strings.Split(d.message, "\n")
		messageRows := visible
		footer = viewKeys(innerWidth, "enter", "confirm", "esc", "cancel")
		if d.kind == dialogDelete {
			if height == 11 || height == 12 {
				// Use the spare overlay row before dropping target names.
				messageRows++
			}
			messageRows -= 2 // Reserve the consequence and confirmation field.
			footer = viewKeys(innerWidth, "enter", "delete", "esc", "cancel")
		}
		for i, line := range message[:max(0, min(len(message), messageRows))] {
			style := viewTextStyle
			if i == 0 {
				style = viewWarnStyle.Bold(true)
			}
			body = append(body, style.Render(viewFit(viewSafe(line), innerWidth)))
		}
		if d.kind == dialogDelete && len(d.fields) > 0 {
			warning := message[len(message)-1]
			if lipgloss.Width(warning) > innerWidth {
				warning = "Permanently deletes files"
			}
			body = append(body, viewWarnStyle.Render(viewFit(warning, innerWidth)))
			body = append(body, dialogInputLine(d.fields[0], true, innerWidth))
		}

	default:
		var focusedLine int
		for i, field := range d.fields {
			if !d.fieldVisible(i) {
				continue
			}
			if i == d.focus {
				focusedLine = len(body)
			}
			body = append(body, dialogInputLine(field, i == d.focus, innerWidth))
			if field.help != "" && visible > len(d.fields) {
				body = append(body, viewMutedStyle.Render(viewFit("  "+viewSafe(field.help), innerWidth)))
			}
		}
		start := viewWindowStart(focusedLine, len(body), visible)
		body = body[start:min(len(body), start+visible)]
		if d.kind == dialogAdd || d.kind == dialogAddResult {
			footer = viewKeys(innerWidth, "tab", "next", "f2", "advanced", "enter", "submit", "esc", "cancel")
		}
	}
	if d.status != "" && height >= 4 {
		body = append(body, viewErrorStyle.Render(viewFit(viewSafe(d.status), innerWidth)))
	}

	title := viewTitleStyle.Render(viewFit(viewSafe(d.title), innerWidth))
	if d.kind == dialogPalette && innerWidth >= 31 {
		title = viewRightAligned(innerWidth, title, viewKeys(innerWidth, "esc", "close"))
	} else if d.kind == dialogPalette {
		footer = viewKeys(innerWidth, "esc", "close", "enter", "run")
	}
	lines := []string{title}
	if height >= 11 {
		lines = append([]string{"", title, ""}, body...)
		lines = append(lines, "", footer, "")
	} else {
		lines = append(lines, body...)
		lines = append(lines, footer)
	}

	padding := strings.Repeat(" ", padX)
	for i, line := range lines {
		lines[i] = padding + viewColumn(line, innerWidth) + padding
	}
	lines = lines[:min(height, len(lines))]
	return viewSurface(strings.Join(lines, "\n"), innerWidth+2*padX, len(lines),
		viewPanelColor, viewTextStyle.GetForeground())
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
	line := viewMutedStyle.Render(prefix+label+": ") + value
	if focused {
		line = viewAccentStyle.Render(prefix+label+": ") + value
	}
	return viewSurface(viewFit(line, width), width, 1, viewFieldColor, viewTextStyle.GetForeground())
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
		return client.FormatSeedtimeLimitInput(torrent.SeedingTimeLimit)
	case "inactive-seedtime-limit":
		return client.FormatSeedtimeLimitInput(client.EffectiveInactiveSeedingTimeLimit(torrent))
	case "share-limits-mode":
		return shareLimitModeValue(torrent.ShareLimitsMode)
	case "share-limit-action":
		return shareLimitActionValue(torrent.ShareLimitAction)
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
