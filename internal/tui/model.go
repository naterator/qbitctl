//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"path/filepath"
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
	pageCount
)

type workKind uint8

const (
	queueWork workKind = iota
	searchWork
	serverWork
	detailWork
	actionWork
)

type service interface {
	queue(context.Context) ([]client.TorrentInfo, error)
	search(context.Context, string, string, time.Duration) ([]client.SearchResult, string, error)
	server(context.Context) ([]detailRow, error)
	details(context.Context, string, bool) ([]detailRow, error)
	action(context.Context, actionRequest) error
}

type workDone struct {
	kind    workKind
	queue   []client.TorrentInfo
	results []client.SearchResult
	rows    []detailRow
	title   string
	notice  string
	err     error
}

type refreshTick struct{}

type model struct {
	ctx                   context.Context
	backend               service
	workers               sync.WaitGroup
	cancel                context.CancelFunc
	work                  workKind
	quitting              bool
	searchPreferencesFile string
	queuePreferencesFile  string

	page          page
	width, height int
	allTorrents   []client.TorrentInfo
	queue         []client.TorrentInfo
	allResults    []client.SearchResult
	results       []client.SearchResult
	serverRows    []detailRow
	queueIndex    int
	resultIndex   int
	marked        map[string]bool
	filter        string
	stateFilter   string
	queueSort     queueSort
	resultFilter  resultFilter
	resultSort    resultSort
	resultEditor  *resultFilterEditor
	query         string
	searchPlugin  string
	searchTimeout time.Duration
	dialog        *dialog
	busy, notice  string
	failed        bool
}

func newModel(ctx context.Context, backend service) *model {
	return &model{
		ctx: ctx, backend: backend, width: 80, height: 24,
		marked: make(map[string]bool), stateFilter: "all",
		queueSort: queueActiveFirst, resultSort: resultOriginal,
		searchPlugin: "jackett", searchTimeout: 30 * time.Second,
	}
}

// Run opens the terminal workspace using the caller's authenticated session.
func Run(ctx context.Context, app *client.App, input io.Reader, output io.Writer) error {
	sessionCtx, stop := context.WithCancel(ctx)
	m := newModel(sessionCtx, &tuiClient{app: app})
	dir, err := tuiPreferencesDir()
	if err != nil {
		m.notice = "TUI preferences are unavailable. " + err.Error()
	} else {
		m.loadSearchPreferences(filepath.Join(dir, searchPreferencesFileName))
		m.loadQueuePreferences(filepath.Join(dir, queuePreferencesFileName))
	}
	defer func() {
		stop()
		// A canceled search still needs time to delete its remote job before
		// the caller logs out the session.
		m.workers.Wait()
	}()
	program := tea.NewProgram(m, tea.WithContext(sessionCtx), tea.WithoutSignalHandler(), tea.WithInput(input), tea.WithOutput(output))
	_, err = program.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), nextTick())
}

func nextTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return refreshTick{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.resultEditor != nil {
			m.resultEditor.inputs[m.resultEditor.active].SetWidth(m.resultFilterInputWidth())
			return m, m.updateResultFilter(msg)
		}
		return m, nil
	case workDone:
		return m, m.finishWork(msg)
	case refreshTick:
		if m.busy == "" && m.dialog == nil && m.page != searchPage && !m.quitting {
			return m, tea.Batch(m.refresh(), nextTick())
		}
		return m, nextTick()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if m.busy != "" && m.work == searchWork && !m.quitting {
				m.cancelSearch()
				return m, nil
			}
			return m, m.requestQuit()
		}
		if m.resultEditor != nil {
			return m, m.updateResultFilter(msg)
		}
		if m.dialog != nil {
			return m, m.updateDialog(msg)
		}
		return m, m.handleKey(msg.String())
	default:
		if m.resultEditor != nil {
			return m, m.updateResultFilter(msg)
		}
		if m.dialog != nil {
			return m, m.updateDialog(msg)
		}
	}
	return m, nil
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
		if m.busy != "" && m.work == searchWork {
			m.cancelSearch()
		} else if m.page == searchPage && m.resultFilter.active() {
			m.clearResultFilter()
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
		m.moveCursor(max(len(m.queue), len(m.results)))
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
		if len(m.serverRows) > 0 {
			m.openDetailDialog("Server & transfers", m.serverRows)
		}
	}
	if m.page == searchPage {
		switch key {
		case "s":
			return m.openAction("sort-results")
		case "f":
			return m.openAction("filter-results")
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
		result.kind = kind
		done <- result
	}()
	return func() tea.Msg { return <-done }
}

func (m *model) finishWork(done workDone) tea.Cmd {
	m.cancel, m.busy = nil, ""
	if m.quitting {
		return tea.Quit
	}
	if done.err != nil {
		m.notice, m.failed = done.err.Error(), true
		return nil
	}
	if done.notice != "" {
		m.notice, m.failed = done.notice, false
	}
	switch done.kind {
	case queueWork:
		m.replaceQueue(done.queue)
	case searchWork:
		m.replaceSearchResults(done.results)
	case serverWork:
		m.serverRows = done.rows
	case detailWork:
		if m.dialog == nil {
			m.openDetailDialog(done.title, done.rows)
		}
	case actionWork:
		if m.page == queuePage {
			return m.refresh()
		}
	}
	if (m.page == serverPage && done.kind != serverWork && m.serverRows == nil) ||
		(m.page == queuePage && done.kind != queueWork && m.allTorrents == nil) {
		return m.refresh()
	}
	return nil
}

func (m *model) refresh() tea.Cmd {
	if m.busy != "" {
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
	default:
		return m.startWork(queueWork, "Refreshing queue", func(ctx context.Context) workDone {
			torrents, err := m.backend.queue(ctx)
			return workDone{queue: torrents, err: err}
		})
	}
}

func (m *model) goTo(next page) tea.Cmd {
	m.page = next
	if next == searchPage {
		return nil
	}
	return m.refresh()
}

func (m *model) launchSearch(query, plugin string, timeout time.Duration) tea.Cmd {
	if m.busy != "" {
		return nil
	}
	if query != m.query || plugin != m.searchPlugin {
		// A new search starts with the same numeric criteria and no text filter.
		m.resultFilter = m.resultFilter.numeric()
	}
	m.page, m.query, m.searchPlugin, m.searchTimeout = searchPage, query, plugin, timeout
	m.replaceSearchResults(nil)
	m.notice, m.failed = "", false
	return m.startWork(searchWork, "Searching "+plugin, func(ctx context.Context) workDone {
		results, notice, err := m.backend.search(ctx, query, plugin, timeout)
		if ctx.Err() != nil {
			if notice != "" {
				notice = "Search canceled · " + notice
			} else {
				notice = "Search canceled"
			}
			return workDone{notice: notice}
		}
		if notice == "" && len(results) == 0 {
			notice = "Search finished with no results"
		}
		return workDone{results: results, notice: notice, err: err}
	})
}

func (m *model) cancelSearch() {
	m.cancel()
	m.busy = "Canceling search"
}

func (m *model) requestQuit() tea.Cmd {
	if m.busy == "" {
		return tea.Quit
	}
	m.quitting = true
	m.cancel()
	m.busy = "Closing session"
	return nil
}

func (m *model) loadDetails(trackers bool) tea.Cmd {
	torrent, ok := m.focusedTorrent()
	if !ok || m.page != queuePage {
		return nil
	}
	label, title := "Loading torrent details", torrent.Name
	if trackers {
		label, title = "Loading trackers", "Trackers · "+torrent.Name
	}
	return m.startWork(detailWork, label, func(ctx context.Context) workDone {
		rows, err := m.backend.details(ctx, torrent.Hash, trackers)
		return workDone{title: title, rows: rows, err: err}
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
		return workDone{notice: notices[request.kind], err: err}
	})
}

func (m *model) currentIndex() int {
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
	m.allTorrents = torrents
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
