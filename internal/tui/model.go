//go:build !plan9

package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type page uint8

const (
	queuePage page = iota
	searchPage
	serverPage
	configPage
	pageCount
)

type workKind uint8

const (
	queueWork workKind = iota
	searchWork
	searchPluginsWork
	serverWork
	detailWork
	detailRefreshWork
	actionWork
	configWork
	connectWork
)

type service interface {
	queue(context.Context) ([]client.TorrentInfo, error)
	search(context.Context, string, string, time.Duration) ([]client.SearchResult, string, error)
	server(context.Context) ([]detailRow, error)
	details(context.Context, string, bool) ([]detailRow, error)
	action(context.Context, actionRequest) error
}

const defaultSearchPluginSelection = client.DefaultSearchPlugins

type searchPluginInfo = client.SearchPlugin

type searchPluginService interface {
	searchPlugins(context.Context) ([]searchPluginInfo, error)
}

// searchCatalogService lets the TUI pass the catalog it just fetched to the
// search adapter. This keeps a validated default from causing a second
// /search/plugins request before the actual search starts. Test backends do
// not need to implement it; service.search remains the compatibility path.
type searchCatalogService interface {
	searchWithCatalog(context.Context, string, string, time.Duration, []searchPluginInfo) ([]client.SearchResult, string, error)
}

type workDone struct {
	kind                      workKind
	queue                     []client.TorrentInfo
	queueChanged              bool
	queueTransfer             queueTransferState
	results                   []client.SearchResult
	rows                      []detailRow
	title                     string
	detailTarget              *torrentDetailTarget
	detailUpdate              *torrentDetailUpdate
	notice                    string
	app                       *client.App
	setup                     setupState
	setDefault                *bool
	setDataUnits              *client.DataUnits
	plugins                   []searchPluginInfo
	pluginGeneration          uint64
	pluginSelectionGeneration uint64
	effectivePlugin           string
	searchPluginWasDefault    bool
	err                       error
}

type refreshTick struct{}

type noticeExpired uint64

type model struct {
	ctx             context.Context
	backend         service
	app             *client.App
	ownsApp         bool
	workers         sync.WaitGroup
	pendingMu       sync.Mutex
	pendingApps     []*client.App
	cancel          context.CancelFunc
	work            workKind
	quitting        bool
	preferencesPath string
	configPath      string
	setup           setupState
	connect         *client.Options
	connectExplicit bool
	connectCanceled bool
	connectTarget   string
	exitErr         error
	theme           themeID
	themePreview    themeID

	page                            page
	width, height                   int
	allTorrents                     []client.TorrentInfo
	queueActive                     bool
	queueTransfer                   queueTransferState
	refreshPaused                   bool
	queue                           []client.TorrentInfo
	allResults                      []client.SearchResult
	results                         []client.SearchResult
	serverRows                      []detailRow
	queueIndex                      int
	resultIndex                     int
	configSelection                 int
	marked                          map[string]bool
	filter                          string
	stateFilter                     string
	queueSort                       queueSort
	resultFilter                    resultFilter
	resultSort                      resultSort
	resultEditor                    *resultFilterEditor
	query                           string
	searchPlugin                    string
	savedSearchPlugin               string
	searchPluginDefaultActive       bool
	searchTimeout                   time.Duration
	searchPlugins                   []searchPluginInfo
	searchPluginsAttempted          bool
	searchPluginsLoading            bool
	searchPluginCancel              context.CancelFunc
	searchPluginsStale              bool
	searchPluginsError              string
	searchPluginGeneration          uint64
	searchPluginSelectionGeneration uint64
	dataUnits                       client.DataUnits
	dataUnitsExplicit               bool
	dialog                          *dialog
	busy, notice                    string
	failed                          bool

	noticeID uint64
}

// connectClient is injectable so connection progress and cancellation can be
// tested without waiting on a real Web API.
var connectClient = client.NewClientWithOutput

func newModel(ctx context.Context, backend service) *model {
	return &model{
		ctx: ctx, backend: backend, width: 80, height: 24,
		marked: make(map[string]bool), stateFilter: "all",
		queueSort: queueActiveFirst, resultSort: resultOriginal,
		searchPlugin:              defaultSearchPluginSelection,
		savedSearchPlugin:         defaultSearchPluginSelection,
		searchPluginDefaultActive: true, searchTimeout: 30 * time.Second,
		dataUnits: client.DataUnitsHuman,
		theme:     defaultThemeID,
	}
}

func (m *model) Init() tea.Cmd {
	commands := []tea.Cmd{m.nextTick(), m.noticeTimer()}
	if m.connect != nil {
		opts := *m.connect
		setup := m.setup
		m.connect = nil
		commands = append(commands, m.startWork(connectWork, "Connecting to "+m.connectTarget, func(ctx context.Context) workDone {
			app, err := connectClient(ctx, &opts, io.Discard, io.Discard)
			return workDone{app: app, err: err, setup: setup}
		}))
	} else {
		commands = append(commands, m.refresh())
	}
	return tea.Batch(commands...)
}

const (
	activeRefreshInterval = 5 * time.Second
	idleRefreshInterval   = 15 * time.Second
)

func (m *model) refreshInterval() time.Duration {
	// Keep the first refresh short so page entry gets a responsive initial
	// snapshot. Once a queue is established, quiet queues can use a less
	// chatty cadence while active transfers retain the five-second updates.
	if m.page == serverPage || m.allTorrents == nil {
		return activeRefreshInterval
	}
	if m.queueActive {
		return activeRefreshInterval
	}
	return idleRefreshInterval
}

func (m *model) nextTick() tea.Cmd {
	return tea.Tick(m.refreshInterval(), func(time.Time) tea.Msg { return refreshTick{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	noticeID := m.noticeID
	cmd := m.update(msg)
	if _, ok := msg.(workDone); ok && m.dialog != nil && m.dialog.kind == dialogPalette {
		m.dialog.allItems = m.paletteCommands()
		m.dialog.refreshPalette()
	}
	if m.noticeID != noticeID {
		cmd = tea.Batch(cmd, m.noticeTimer())
	}
	return m, cmd
}

func (m *model) setNotice(text string, failed bool) {
	m.notice, m.failed = text, failed
	m.noticeID++
}

func (m *model) noticeTimer() tea.Cmd {
	if m.notice == "" {
		return nil
	}
	expiry := noticeExpired(m.noticeID)
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return expiry })
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.resultEditor != nil {
			m.resultEditor.inputs[m.resultEditor.active].SetWidth(m.resultFilterInputWidth())
			return m.updateResultFilter(msg)
		}
		return nil
	case workDone:
		return m.finishWork(msg)
	case noticeExpired:
		if uint64(msg) == m.noticeID {
			m.setNotice("", false)
		}
		return nil
	case refreshTick:
		if !m.refreshPaused && m.busy == "" && !m.quitting {
			if m.dialog != nil {
				return tea.Batch(m.refreshTorrentDetails(), m.nextTick())
			}
			if m.page != searchPage && m.page != configPage {
				return tea.Batch(m.refresh(), m.nextTick())
			}
		}
		return m.nextTick()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if !m.quitting && m.busy != "" && m.work == searchWork {
				m.cancelSearch()
				return nil
			}
			if !m.quitting && m.searchPluginsLoading && m.dialog != nil && m.dialog.kind == dialogSearchPlugins {
				m.cancelSearchPlugins()
				return nil
			}
			return m.requestQuit()
		}
		if m.resultEditor != nil {
			return m.updateResultFilter(msg)
		}
		if m.dialog != nil {
			return m.updateDialog(msg)
		}
		return m.handleKey(msg.String())
	default:
		if m.resultEditor != nil {
			return m.updateResultFilter(msg)
		}
		if m.dialog != nil {
			return m.updateDialog(msg)
		}
	}
	return nil
}

func (m *model) handleKey(key string) tea.Cmd {
	switch key {
	case "tab":
		return m.goTo((m.page + 1) % pageCount)
	case "shift+tab":
		return m.goTo((m.page + pageCount - 1) % pageCount)
	case "1":
		return m.goTo(queuePage)
	case "2":
		return m.goTo(searchPage)
	case "3":
		return m.goTo(serverPage)
	case "4":
		return m.goTo(configPage)
	case "ctrl+p", ":":
		return m.openAction("palette")
	case "?":
		return m.openAction("help")
	case "q", "ctrl+q":
		return m.requestQuit()
	case "r":
		return m.refresh()
	case "/":
		if m.page == queuePage {
			return m.openAction("filter")
		}
		return m.openAction("search")
	case "esc":
		if m.busy != "" && m.work == connectWork {
			return m.openAction("cancel-connection")
		} else if m.busy != "" && m.work == searchWork {
			return m.openAction("cancel-search")
		} else if m.searchPluginsLoading && m.dialog != nil && m.dialog.kind == dialogSearchPlugins {
			m.cancelSearchPlugins()
		} else if m.page == searchPage && m.resultFilter.active() {
			return m.openAction("clear-result-filter")
		} else if m.page == queuePage && len(m.marked) > 0 {
			m.clearMarks()
		}
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.tableHeight())
	case "pgdown":
		m.moveCursor(m.tableHeight())
	case "home", "g":
		m.moveCursor(-m.currentIndex())
	case "end", "shift+g", "G":
		if m.page == configPage {
			m.moveConfigSelection(len(m.configSettings()))
		} else {
			m.moveCursor(max(len(m.queue), len(m.results)))
		}
	case "a":
		return m.openAction("add")
	case "enter":
		if m.page == queuePage {
			return m.loadDetails(false)
		}
		if m.page == searchPage {
			if _, ok := m.focusedResult(); ok {
				return m.openAction("add-result")
			}
			return m.openAction("search")
		}
		if m.page == configPage {
			return m.activateConfigSetting()
		}
		return m.openAction("server-details")
	}
	if m.page == configPage {
		return nil
	}
	if m.page == searchPage {
		switch key {
		case "s":
			return m.openAction("sort-results")
		case "f":
			return m.openAction("filter-results")
		case "p":
			return m.openAction("providers")
		}
	}
	if m.page != queuePage {
		return nil
	}
	switch key {
	case "space", " ":
		m.toggleMark()
	case "e":
		return m.openAction("edit")
	case "o":
		return m.openAction("sort-queue")
	case "s":
		return m.openAction("start")
	case "p":
		return m.openAction("pause")
	case "f":
		return m.openAction("force-start")
	case "m":
		return m.openAction("move")
	case "t":
		return m.loadDetails(true)
	case "x":
		return m.openAction("remove")
	case "D", "shift+d":
		return m.openAction("delete")
	}
	return nil
}

func (m *model) startWork(kind workKind, label string, task func(context.Context) workDone) tea.Cmd {
	if m.busy != "" || m.quitting {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.work, m.busy = cancel, kind, label
	done := make(chan workDone, 1)
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()
		result := task(ctx)
		if result.app != nil {
			m.pendingMu.Lock()
			m.pendingApps = append(m.pendingApps, result.app)
			m.pendingMu.Unlock()
		}
		result.kind = kind
		done <- result
	}()
	return func() tea.Msg { return <-done }
}

func (m *model) finishWork(done workDone) tea.Cmd {
	if done.kind == searchPluginsWork {
		if done.pluginGeneration != m.searchPluginGeneration {
			if m.quitting {
				return tea.Quit
			}
			return nil
		}
		m.searchPluginCancel = nil
		m.searchPluginsLoading = false
		if m.quitting {
			return tea.Quit
		}
		selectionChanged := done.pluginSelectionGeneration != m.searchPluginSelectionGeneration
		if errors.Is(done.err, context.Canceled) {
			m.searchPluginsStale = len(m.searchPlugins) > 0
			if !selectionChanged {
				m.revalidateSavedSearchPlugin(true)
			}
			return nil
		}
		if done.err != nil {
			m.searchPluginsStale = len(m.searchPlugins) > 0
			m.searchPluginsError = done.err.Error()
			if !selectionChanged {
				m.revalidateSavedSearchPlugin(true)
			}
			m.refreshSearchDialogPlugin()
			return nil
		}
		m.searchPlugins = normalizeSearchPlugins(done.plugins)
		m.searchPluginsAttempted = true
		m.searchPluginsStale = false
		m.searchPluginsError = ""
		if !selectionChanged {
			m.revalidateSavedSearchPlugin(false)
		}
		m.refreshSearchDialogPlugin()
		m.refreshSearchPluginPicker()
		return nil
	}
	m.cancel, m.busy = nil, ""
	if m.quitting {
		m.releasePendingApp(done.app)
		if done.app != nil {
			done.app.Logout()
		}
		return tea.Quit
	}
	m.releasePendingApp(done.app)
	if done.kind == connectWork {
		canceled := m.connectCanceled
		m.connectCanceled = false
		if canceled || errors.Is(done.err, context.Canceled) {
			return m.finishCanceledConnection(done.app)
		}
		if done.err != nil {
			if done.app != nil {
				done.app.Logout()
			}
			if m.connectExplicit && !canceled {
				m.exitErr = done.err
				return tea.Quit
			}
			m.page = configPage
			m.setNotice("Connection failed: "+done.err.Error(), true)
			m.openConfigDialog()
			if m.dialog != nil {
				m.dialog.status = "Connection failed: " + done.err.Error()
			}
			return nil
		}
		if done.app == nil {
			m.page = configPage
			m.setNotice("Connection failed: client returned no session", true)
			m.openConfigDialog()
			if m.dialog != nil {
				m.dialog.status = "Connection failed: client returned no session"
			}
			return nil
		}
		m.adoptApp(done.app, done.setup)
		return m.refresh()
	}
	if done.kind == detailRefreshWork {
		m.finishTorrentDetailsRefresh(done)
		return nil
	}
	if done.err != nil {
		if done.kind == searchWork {
			m.applySearchPluginResult(done)
		}
		if done.app != nil {
			done.app.Logout()
		}
		m.setNotice(done.err.Error(), true)
		return nil
	}
	if done.notice != "" {
		m.setNotice(done.notice, false)
	}
	switch done.kind {
	case queueWork:
		m.queueTransfer = done.queueTransfer
		if done.queueChanged || m.allTorrents == nil {
			m.replaceQueue(done.queue)
		}
	case searchWork:
		m.applySearchPluginResult(done)
		m.replaceSearchResults(done.results)
	case serverWork:
		m.serverRows = done.rows
	case detailWork:
		if m.dialog == nil && m.page == queuePage {
			m.openDetailDialog(done.title, done.rows)
			m.dialog.torrentDetails = done.detailTarget
		}
	case actionWork:
		if m.page == queuePage {
			return m.refresh()
		}
	case configWork:
		if done.setDefault != nil {
			m.setup.tuiDefault = *done.setDefault
			m.setNotice("TUI startup preference saved", false)
		}
		if done.setDataUnits != nil {
			m.applyDataUnits(*done.setDataUnits)
			m.setNotice("Data units saved", false)
			if m.dialog != nil && m.dialog.torrentDetails != nil {
				return m.refreshTorrentDetails()
			}
			if m.page == serverPage {
				return m.refresh()
			}
		}
		if done.app != nil {
			if m.app != nil && m.app != done.app {
				// No request worker can be active while the config dialog is
				// submitted. Release the old Web UI session before replacing it.
				m.app.Logout()
			}
			m.adoptApp(done.app, done.setup)
			m.setNotice("Configuration saved · reconnecting", false)
			return m.refresh()
		}
	}
	if (m.page == serverPage && done.kind != serverWork && m.serverRows == nil) ||
		(m.page == queuePage && done.kind != queueWork && m.allTorrents == nil) {
		return m.refresh()
	}
	return nil
}

func (m *model) applySearchPluginResult(done workDone) {
	if !done.searchPluginWasDefault || done.pluginSelectionGeneration != m.searchPluginSelectionGeneration {
		return
	}
	if done.plugins != nil {
		m.searchPlugins = normalizeSearchPlugins(done.plugins)
		m.searchPluginsAttempted = true
		m.searchPluginsStale = false
		m.searchPluginsError = ""
	}
	if done.effectivePlugin != "" {
		m.searchPlugin = done.effectivePlugin
	}
}

func (m *model) applyDataUnits(mode client.DataUnits) {
	if mode != client.DataUnitsBytes {
		mode = client.DataUnitsHuman
	}
	m.dataUnits = mode
	if backend, ok := m.backend.(interface{ setDataUnits(client.DataUnits) }); ok {
		backend.setDataUnits(mode)
	}
}

func (m *model) dataUnitsLabel() string {
	if m.dataUnits == client.DataUnitsBytes {
		return "Bytes"
	}
	return "Human-readable"
}

func (m *model) finishCanceledConnection(app *client.App) tea.Cmd {
	if app != nil {
		app.Logout()
	}
	m.page = configPage
	m.setNotice("Connection canceled", true)
	m.openConfigDialog()
	if m.dialog != nil {
		m.dialog.status = "Connection canceled"
	}
	return nil
}

func (m *model) adoptApp(app *client.App, setup setupState) {
	m.app, m.ownsApp = app, true
	m.backend = &tuiClient{app: app}
	if m.dataUnitsExplicit {
		m.applyDataUnits(m.dataUnits)
	} else {
		m.applyDataUnits(app.DataUnits)
	}
	m.configPath = setup.path
	if m.configPath == "" {
		m.configPath = app.ConfigPath()
	}
	m.setup = setup
	m.resetRemoteState()
	m.loadPreferences(m.configPath)
	m.page = queuePage
}

func (m *model) releasePendingApp(app *client.App) {
	if app == nil {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	for i, pending := range m.pendingApps {
		if pending == app {
			m.pendingApps = append(m.pendingApps[:i], m.pendingApps[i+1:]...)
			return
		}
	}
}

func (m *model) cleanupPendingApps() {
	m.pendingMu.Lock()
	pending := append([]*client.App(nil), m.pendingApps...)
	m.pendingApps = nil
	m.pendingMu.Unlock()
	for _, app := range pending {
		if app != nil {
			app.Logout()
		}
	}
}

func (m *model) refresh() tea.Cmd {
	if m.busy != "" {
		return nil
	}
	if m.backend == nil {
		return nil
	}
	switch m.page {
	case serverPage:
		return m.startWork(serverWork, "Reading server info", func(ctx context.Context) workDone {
			rows, err := m.backend.server(ctx)
			return workDone{rows: rows, err: err}
		})
	case searchPage:
		if m.query == "" {
			return m.openAction("search")
		}
		return m.launchSearch(m.query, m.searchPlugin, m.searchTimeout)
	case configPage:
		return nil
	default:
		backend := m.backend
		return m.startWork(queueWork, "Refreshing queue", func(ctx context.Context) workDone {
			return fetchQueueUpdate(ctx, backend)
		})
	}
}

func (m *model) goTo(next page) tea.Cmd {
	m.page = next
	if next == searchPage {
		// A named saved provider is checked against the current server catalog
		// every time Search is entered. The catalog is still loaded lazily, so
		// the saved name remains visible while the request is in flight.
		return m.ensureSearchPlugins(m.needsDefaultSearchValidation())
	}
	if next == configPage {
		if m.needsDefaultSearchValidation() {
			return m.ensureSearchPlugins()
		}
		return nil
	}
	if m.backend == nil {
		return nil
	}
	return m.refresh()
}

func (m *model) ensureSearchPlugins(force ...bool) tea.Cmd {
	refresh := len(force) > 0 && force[0]
	if m.backend == nil || m.searchPluginsLoading || (!refresh && m.searchPluginsAttempted) {
		return nil
	}
	provider, ok := m.backend.(searchPluginService)
	if !ok {
		m.searchPluginsAttempted = true
		m.revalidateSavedSearchPlugin(true)
		return nil
	}
	if refresh {
		m.searchPluginsAttempted = false
		m.searchPluginsError = ""
	}
	generation := m.searchPluginGeneration
	selectionGeneration := m.searchPluginSelectionGeneration
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	m.searchPluginCancel = cancel
	m.searchPluginsLoading = true
	done := make(chan workDone, 1)
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()
		plugins, err := provider.searchPlugins(ctx)
		done <- workDone{kind: searchPluginsWork, plugins: plugins, pluginGeneration: generation, pluginSelectionGeneration: selectionGeneration, err: err}
	}()
	return func() tea.Msg { return <-done }
}

func (m *model) launchSearch(query, plugin string, timeout time.Duration) tea.Cmd {
	if m.busy != "" {
		return nil
	}
	query = strings.TrimSpace(query)
	plugin = strings.TrimSpace(plugin)
	if plugin == "" {
		plugin = defaultSearchPluginSelection
	}
	if query != m.query || plugin != m.searchPlugin {
		// A new search starts with the same numeric criteria and no text filter.
		m.resultFilter = m.resultFilter.numeric()
	}
	followsDefault := m.searchSelectionFollowsDefault(plugin)
	validateSaved := followsDefault && m.needsDefaultSearchValidation()
	savedSelection := normalizeSavedSearchPlugin(m.savedSearchPlugin)
	selectionGeneration := m.searchPluginSelectionGeneration
	backend := m.backend
	provider, hasProvider := backend.(searchPluginService)
	searcher, hasCatalogSearcher := backend.(searchCatalogService)
	m.page, m.query, m.searchPlugin, m.searchTimeout = searchPage, query, plugin, timeout
	m.searchPluginDefaultActive = followsDefault
	m.replaceSearchResults(nil)
	m.setNotice("", false)
	return m.startWork(searchWork, "Searching "+m.searchPluginLabel(plugin), func(ctx context.Context) workDone {
		effectivePlugin := plugin
		var freshCatalog []searchPluginInfo
		if validateSaved {
			if hasProvider {
				plugins, discoveryErr := provider.searchPlugins(ctx)
				if ctx.Err() != nil {
					return workDone{notice: "Search canceled"}
				}
				if discoveryErr == nil {
					freshCatalog = normalizeSearchPlugins(plugins)
					effectivePlugin = client.ValidateDefaultSearchPlugin(savedSelection, freshCatalog)
				} else {
					effectivePlugin = defaultSearchPluginSelection
				}
			} else {
				effectivePlugin = defaultSearchPluginSelection
			}
		}
		var results []client.SearchResult
		var notice string
		var err error
		if hasCatalogSearcher && freshCatalog != nil && len(freshCatalog) > 0 {
			results, notice, err = searcher.searchWithCatalog(ctx, query, effectivePlugin, timeout, freshCatalog)
		} else {
			results, notice, err = backend.search(ctx, query, effectivePlugin, timeout)
		}
		metadata := workDone{
			results: results, notice: notice, err: err,
			effectivePlugin:           effectivePlugin,
			searchPluginWasDefault:    followsDefault,
			pluginSelectionGeneration: selectionGeneration,
		}
		if freshCatalog != nil {
			metadata.plugins = freshCatalog
		}
		if ctx.Err() != nil {
			if notice != "" {
				notice = "Search canceled · " + notice
			} else {
				notice = "Search canceled"
			}
			metadata.notice = notice
			metadata.err = nil
			return metadata
		}
		if notice == "" && len(results) == 0 {
			notice = "Search finished with no results"
		}
		metadata.notice = notice
		return metadata
	})
}

func (m *model) searchSelectionFollowsDefault(selection string) bool {
	selection = normalizeSavedSearchPlugin(selection)
	if m.searchPluginDefaultActive && selection == normalizeSavedSearchPlugin(m.searchPlugin) {
		return true
	}
	return selection == normalizeSavedSearchPlugin(m.savedSearchPlugin)
}

func (m *model) needsDefaultSearchValidation() bool {
	saved := normalizeSavedSearchPlugin(m.savedSearchPlugin)
	return saved != defaultSearchPluginSelection && saved != "all"
}

// revalidateSavedSearchPlugin updates only the effective provider. The saved
// value is deliberately left untouched when a plugin is missing, disabled, or
// the catalog request failed, so a later refresh can recover it automatically.
func (m *model) revalidateSavedSearchPlugin(unavailable bool) {
	if !m.searchPluginDefaultActive {
		return
	}
	var catalog []searchPluginInfo
	if !unavailable && m.searchPluginsAttempted && !m.searchPluginsStale {
		catalog = m.searchPlugins
	}
	m.searchPlugin = client.ValidateDefaultSearchPlugin(m.savedSearchPlugin, catalog)
}

func (m *model) cancelSearch() {
	m.cancel()
	m.busy = "Canceling search"
}

func (m *model) cancelSearchPlugins() {
	if m.searchPluginCancel != nil {
		m.searchPluginCancel()
	}
}

func normalizeSearchPlugins(input []searchPluginInfo) []searchPluginInfo {
	plugins := make([]searchPluginInfo, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, plugin := range input {
		plugin.Name = strings.TrimSpace(plugin.Name)
		plugin.FullName = strings.TrimSpace(plugin.FullName)
		if plugin.Name == "" {
			continue
		}
		key := plugin.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		plugins = append(plugins, plugin)
	}
	return plugins
}

func (m *model) requestQuit() tea.Cmd {
	if m.busy == "" && !m.searchPluginsLoading {
		return tea.Quit
	}
	m.quitting = true
	if m.cancel != nil {
		m.cancel()
	}
	if m.searchPluginCancel != nil {
		m.searchPluginCancel()
	}
	m.busy = "Closing session"
	return nil
}

func (m *model) loadDetails(trackers bool) tea.Cmd {
	torrent, ok := m.focusedTorrent()
	if !ok || m.page != queuePage || m.backend == nil {
		return nil
	}
	label := "Loading torrent details"
	if trackers {
		label = "Loading trackers"
	}
	backend := m.backend
	target := torrentDetailTarget{hash: torrent.Hash, trackers: trackers}
	return m.startWork(detailWork, label, func(ctx context.Context) workDone {
		rows, err := backend.details(ctx, target.hash, target.trackers)
		return workDone{title: target.title(torrent.Name), rows: rows, detailTarget: &target, err: err}
	})
}

func (m *model) launchAction(label string, request actionRequest) tea.Cmd {
	return m.startWork(actionWork, label, func(ctx context.Context) workDone {
		err := m.backend.action(ctx, request)
		notices := map[string]string{
			"add": "Torrent submitted", "add-result": "Search result submitted", "start": "Started selected torrents",
			"pause": "Paused selected torrents", "force-start": "Force started selected torrents", "move": "Move requested",
			"remove": "Removed selected torrents", "delete": "Deleted selected torrents and files", "set": "Saved torrent property",
		}
		notice := notices[request.kind]
		if request.kind == "set" {
			count := len(strings.Split(request.hashes, "|"))
			if count > 1 {
				notice = fmt.Sprintf("Saved %s for %d torrents", request.field, count)
			}
		}
		return workDone{notice: notice, err: err}
	})
}

func (m *model) currentIndex() int {
	if m.page == configPage {
		return m.configSelection
	}
	if m.page == searchPage {
		return m.resultIndex
	}
	return m.queueIndex
}

func (m *model) moveCursor(delta int) {
	switch m.page {
	case queuePage:
		m.queueIndex = max(0, min(m.queueIndex+delta, len(m.queue)-1))
	case searchPage:
		m.resultIndex = max(0, min(m.resultIndex+delta, len(m.results)-1))
	case configPage:
		m.moveConfigSelection(delta)
	}
}

func (m *model) focusedTorrent() (client.TorrentInfo, bool) {
	if m.queueIndex >= 0 && m.queueIndex < len(m.queue) {
		return m.queue[m.queueIndex], true
	}
	return client.TorrentInfo{}, false
}

func (m *model) focusedResult() (client.SearchResult, bool) {
	if m.resultIndex >= 0 && m.resultIndex < len(m.results) {
		return m.results[m.resultIndex], true
	}
	return client.SearchResult{}, false
}

func (m *model) toggleMark() {
	if torrent, ok := m.focusedTorrent(); ok {
		if m.marked[torrent.Hash] {
			delete(m.marked, torrent.Hash)
		} else {
			m.marked[torrent.Hash] = true
		}
	}
}

func (m *model) clearMarks() { clear(m.marked) }

func (m *model) targetHashes() []string {
	if len(m.marked) == 0 {
		if torrent, ok := m.focusedTorrent(); ok {
			return []string{torrent.Hash}
		}
		return nil
	}
	hashes := make([]string, 0, len(m.marked))
	for _, torrent := range m.queue {
		if m.marked[torrent.Hash] {
			hashes = append(hashes, torrent.Hash)
		}
	}
	return hashes
}

func (m *model) replaceQueue(torrents []client.TorrentInfo) {
	if torrents == nil {
		// A successful empty snapshot is still an established, idle queue. Keep
		// it distinct from the nil initial state so adaptive refresh can slow
		// down after the first response.
		torrents = []client.TorrentInfo{}
	}
	m.allTorrents = torrents
	m.queueActive = false
	for _, torrent := range torrents {
		if torrent.DLSpeed > 0 || torrent.UpSpeed > 0 {
			m.queueActive = true
			break
		}
	}
	m.rebuildQueue()
	visible := make(map[string]bool, len(m.queue))
	for _, torrent := range m.queue {
		visible[torrent.Hash] = true
	}
	for hash := range m.marked {
		if !visible[hash] {
			delete(m.marked, hash)
		}
	}
}

func (m *model) setFilter(query, state string) {
	query, state = strings.TrimSpace(query), strings.TrimSpace(state)
	if state == "" {
		state = "all"
	}
	if m.filter != query || m.stateFilter != state {
		m.clearMarks()
	}
	m.filter, m.stateFilter = query, state
	m.rebuildQueue()
}

func (m *model) rebuildQueue() {
	previous, _ := m.focusedTorrent()
	query := strings.Fields(strings.ToLower(m.filter))
	m.queue = m.queue[:0]
	for _, torrent := range m.allTorrents {
		if !matchesState(torrent, m.stateFilter) {
			continue
		}
		index := strings.ToLower(strings.Join([]string{torrent.Name, torrent.Hash, torrent.Category, torrent.Tags}, " "))
		match := true
		for _, term := range query {
			if !strings.Contains(index, term) {
				match = false
				break
			}
		}
		if match {
			m.queue = append(m.queue, torrent)
		}
	}
	slices.SortStableFunc(m.queue, m.queueSort.compare)
	m.queueIndex = max(0, min(m.queueIndex, len(m.queue)-1))
	for i, torrent := range m.queue {
		if previous.Hash == torrent.Hash {
			m.queueIndex = i
			break
		}
	}
}

func matchesState(torrent client.TorrentInfo, filter string) bool {
	state := strings.ToLower(torrent.State)
	stopped := strings.HasPrefix(state, "stopped") || strings.HasPrefix(state, "paused")
	switch strings.ToLower(filter) {
	case "", "all", "any":
		return true
	case "active":
		return torrent.DLSpeed > 0 || torrent.UpSpeed > 0
	case "downloading":
		return !stopped && (strings.HasSuffix(state, "dl") || state == "downloading")
	case "seeding", "uploading":
		return !stopped && (strings.HasSuffix(state, "up") || state == "uploading" || state == "seeding")
	case "stopped", "paused":
		return stopped
	case "error":
		return state == "error" || state == "missingfiles" || state == "unknown"
	default:
		return strings.Contains(state, strings.ToLower(filter))
	}
}
