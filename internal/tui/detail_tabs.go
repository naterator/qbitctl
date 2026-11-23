//go:build !plan9

package tui

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type detailTab uint8

const (
	detailTabOverview detailTab = iota
	detailTabTrackers
	detailTabFiles
	detailTabPeers
	detailTabCount
)

type detailDialogState struct {
	tab              detailTab
	pendingTab       *detailTab
	files            []client.TorrentFile
	peers            client.TorrentPeers
	fileCursor       int
	peerCursor       int
	fileMarks        map[int]bool
	fileFilter       string
	fileFilterActive bool
}

type detailTabUpdate struct {
	dialog   *dialog
	tab      detailTab
	trackers []client.TrackerEntry
	files    []client.TorrentFile
	peers    client.TorrentPeers
	err      error
}

func newDetailDialogState() *detailDialogState {
	return &detailDialogState{tab: detailTabOverview, fileMarks: make(map[int]bool)}
}

func (c *tuiClient) files(ctx context.Context, hash string) ([]client.TorrentFile, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	return c.withContext(ctx, nil, nil).FetchTorrentFilesContext(ctx, hash, nil)
}

func (c *tuiClient) peers(ctx context.Context, hash string, rid int64) (client.TorrentPeers, error) {
	if c == nil || c.app == nil {
		return client.TorrentPeers{}, fmt.Errorf("qBittorrent client is not configured")
	}
	return c.withContext(ctx, nil, nil).FetchTorrentPeersContext(ctx, hash, rid)
}

func (c *tuiClient) setFilePriority(ctx context.Context, hash string, ids []int, priority client.FilePriority) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("qBittorrent client is not configured")
	}
	return c.withContext(ctx, nil, nil).SetFilePriority(hash, ids, priority)
}

func (c *tuiClient) renameFile(ctx context.Context, hash, oldPath, newPath string) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("qBittorrent client is not configured")
	}
	return c.withContext(ctx, nil, nil).RenameTorrentFile(hash, oldPath, newPath)
}

func (c *tuiClient) renameFolder(ctx context.Context, hash, oldPath, newPath string) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("qBittorrent client is not configured")
	}
	return c.withContext(ctx, nil, nil).RenameTorrentFolder(hash, oldPath, newPath)
}

func (m *model) detailState(d *dialog) *detailDialogState {
	if d.detail == nil {
		d.detail = newDetailDialogState()
	}
	return d.detail
}

func detailTabLabel(tab detailTab) string {
	switch tab {
	case detailTabTrackers:
		return "Trackers"
	case detailTabFiles:
		return "Files"
	case detailTabPeers:
		return "Peers"
	default:
		return "Overview"
	}
}

func detailTabHeader(tab detailTab, width int) string {
	parts := make([]string, 0, detailTabCount)
	for current := detailTabOverview; current < detailTabCount; current++ {
		label := fmt.Sprintf("%d %s", int(current)+1, detailTabLabel(current))
		if current == tab {
			parts = append(parts, viewAccentStyle.Render("▸ "+label))
		} else {
			parts = append(parts, viewMutedStyle.Render("  "+label))
		}
	}
	return viewFit(strings.Join(parts, "   "), width)
}

func (m *model) detailBodyLines(d *dialog, width int) []string {
	state := m.detailState(d)
	switch state.tab {
	case detailTabFiles:
		state.fileCursor = min(max(0, len(filteredDetailFiles(state.files, state.fileFilter))-1), max(0, state.fileCursor))
		return m.detailFileLines(state, width)
	case detailTabPeers:
		state.peerCursor = min(max(0, len(state.peers.Peers)-1), max(0, state.peerCursor))
		return m.detailPeerLines(state, width)
	default:
		return dialogDetailLines(d.rows, width)
	}
}

func detailBodyHeight(d *dialog, height int) int {
	return max(1, d.bodyHeight(height)-2)
}

func (m *model) detailFileLines(state *detailDialogState, width int) []string {
	files := filteredDetailFiles(state.files, state.fileFilter)
	if len(files) == 0 {
		if state.fileFilter != "" {
			return []string{viewMutedStyle.Render("No files match /" + viewSafe(state.fileFilter))}
		}
		return []string{viewMutedStyle.Render("No files available")}
	}
	lines := make([]string, 0, len(files)+1)
	if state.fileFilter != "" || state.fileFilterActive {
		lines = append(lines, viewMutedStyle.Render(viewFit("Filter: /"+viewSafe(state.fileFilter), width)))
	}
	for i, file := range files {
		prefix := "  "
		if i == state.fileCursor {
			prefix = "› "
		}
		mark := " "
		if state.fileMarks[file.Index] {
			mark = "✓"
		}
		name := client.SanitizeServerText(file.Name)
		line := fmt.Sprintf("%s%s %d  %-7s %5.1f%%  %s  %s", prefix, mark, file.Index,
			client.FilePriority(file.Priority).String(), file.Progress*100,
			m.dataUnits.FormatSize(file.Size), name)
		line = viewFit(viewSafe(line), width)
		if i == state.fileCursor {
			line = viewSelectedSty.Width(width).Render(line)
		} else if state.fileMarks[file.Index] {
			line = viewAccentStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func filteredDetailFiles(files []client.TorrentFile, filter string) []client.TorrentFile {
	filter = strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]client.TorrentFile, 0, len(files))
	for _, file := range files {
		if filter != "" && !strings.Contains(strings.ToLower(file.Name), filter) {
			continue
		}
		filtered = append(filtered, file)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Index != filtered[j].Index {
			return filtered[i].Index < filtered[j].Index
		}
		return filtered[i].Name < filtered[j].Name
	})
	return filtered
}

func (m *model) detailPeerLines(state *detailDialogState, width int) []string {
	keys := make([]string, 0, len(state.peers.Peers))
	for key := range state.peers.Peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return []string{viewMutedStyle.Render("No peers available")}
	}
	lines := make([]string, 0, len(keys)+1)
	for i, key := range keys {
		peer := state.peers.Peers[key]
		host := client.SanitizeServerText(peer.IP)
		if host == "" {
			host = client.SanitizeServerText(peer.I2PDestination)
		}
		if host == "" {
			host = client.SanitizeServerText(key)
		}
		if peer.Port > 0 && peer.IP != "" {
			host = net.JoinHostPort(host, strconv.Itoa(peer.Port))
		}
		line := fmt.Sprintf("%s%s  %s  %.1f%%  ↓ %s  ↑ %s", map[bool]string{true: "› ", false: "  "}[i == state.peerCursor], host,
			client.SanitizeServerText(peer.Client), peer.Progress*100,
			m.dataUnits.FormatRate(peer.DLSpeed), m.dataUnits.FormatRate(peer.UPSpeed))
		line = viewFit(viewSafe(line), width)
		if i == state.peerCursor {
			line = viewSelectedSty.Width(width).Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *model) updateTorrentDetailKey(d *dialog, name string) (bool, tea.Cmd) {
	if d.torrentDetails == nil {
		return false, nil
	}
	state := m.detailState(d)
	if state.fileFilterActive && state.tab == detailTabFiles {
		switch name {
		case "enter", "return", "esc", "escape":
			state.fileFilterActive = false
			return true, nil
		case "backspace", "ctrl+h":
			runes := []rune(state.fileFilter)
			if len(runes) > 0 {
				state.fileFilter = string(runes[:len(runes)-1])
			}
			state.fileCursor = 0
			m.ensureDetailCursorVisible(d)
			return true, nil
		case "space", " ":
			state.fileFilter += " "
			state.fileCursor = 0
			m.ensureDetailCursorVisible(d)
			return true, nil
		default:
			if name != "" && allPrintable(name) {
				state.fileFilter += name
				state.fileCursor = 0
				m.ensureDetailCursorVisible(d)
				return true, nil
			}
		}
	}
	switch name {
	case "1":
		return true, m.loadDetailTab(detailTabOverview)
	case "o":
		return true, m.loadDetailTab(detailTabOverview)
	case "2":
		return true, m.loadDetailTab(detailTabTrackers)
	case "3":
		return true, m.loadDetailTab(detailTabFiles)
	case "4":
		return true, m.loadDetailTab(detailTabPeers)
	case "tab":
		tab := state.tab
		if state.pendingTab != nil {
			tab = *state.pendingTab
		}
		return true, m.loadDetailTab((tab + 1) % detailTabCount)
	}
	if state.tab == detailTabTrackers {
		return true, m.updateTrackerTab(name)
	}
	if state.tab == detailTabFiles {
		files := filteredDetailFiles(state.files, state.fileFilter)
		switch name {
		case "up", "k":
			state.fileCursor = max(0, state.fileCursor-1)
			m.ensureDetailCursorVisible(d)
			return true, nil
		case "down", "j":
			state.fileCursor = min(max(0, len(files)-1), state.fileCursor+1)
			m.ensureDetailCursorVisible(d)
			return true, nil
		case "space":
			if len(files) > 0 && state.fileCursor < len(files) {
				index := files[state.fileCursor].Index
				state.fileMarks[index] = !state.fileMarks[index]
			}
			return true, nil
		case "/":
			state.fileFilterActive = true
			return true, nil
		case "f2", "f3":
			return true, m.openTorrentPathRename(d, files, name == "f3")
		case "0", "6", "7", "n":
			if name == "n" {
				name = "1"
			}
			priority, _ := client.ParseFilePriority(name)
			return true, m.setDetailFilePriority(state, files, priority)
		}
	}
	if state.tab == detailTabPeers {
		count := len(state.peers.Peers)
		switch name {
		case "up", "k":
			state.peerCursor = max(0, state.peerCursor-1)
			m.ensureDetailCursorVisible(d)
			return true, nil
		case "down", "j":
			state.peerCursor = min(max(0, count-1), state.peerCursor+1)
			m.ensureDetailCursorVisible(d)
			return true, nil
		case "b", "B":
			return true, m.openPeerBanDialog(d)
		}
	}
	return false, nil
}

func (m *model) ensureDetailCursorVisible(d *dialog) {
	if d == nil || d.detail == nil {
		return
	}
	visible := detailBodyHeight(d, m.heightForDialog())
	innerWidth, _ := dialogDimensions(m.width)
	lines := m.detailBodyLines(d, innerWidth)
	line := 0
	if d.detail.tab == detailTabFiles {
		line += d.detail.fileCursor
		if d.detail.fileFilter != "" || d.detail.fileFilterActive {
			line++
		}
	} else if d.detail.tab == detailTabPeers {
		line += d.detail.peerCursor
	}
	if line < d.scroll {
		d.scroll = line
	} else if line >= d.scroll+visible {
		d.scroll = line - visible + 1
	}
	d.scroll = min(max(0, d.scroll), max(0, len(lines)-visible))
}

func allPrintable(value string) bool {
	if len([]rune(value)) != 1 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return value != ""
}

func (m *model) loadDetailTab(tab detailTab) tea.Cmd {
	d := m.dialog
	if d == nil || d.torrentDetails == nil {
		return nil
	}
	state := m.detailState(d)
	if m.busy != "" {
		if m.work == detailTabWork || m.work == detailRefreshWork {
			state.pendingTab = &tab
		} else {
			d.status = "Finish the current request first"
		}
		return nil
	}
	state.pendingTab = nil
	state.tab = tab
	state.fileCursor, state.peerCursor = 0, 0
	d.scroll = 0
	d.status = ""
	if tab == detailTabOverview && d.rows != nil {
		return nil
	}
	backend, target := m.backend, *d.torrentDetails
	update := &detailTabUpdate{dialog: d, tab: tab}
	switch tab {
	case detailTabOverview:
		return m.startWork(detailTabWork, "Loading torrent overview", func(ctx context.Context) workDone {
			rows, err := backend.details(ctx, target.hash, false)
			return workDone{rows: rows, detailTabUpdate: update, err: err}
		})
	case detailTabTrackers:
		source, ok := backend.(trackerService)
		if !ok {
			d.status = "Tracker details are unavailable for this connection"
			return nil
		}
		return m.startWork(detailTabWork, "Loading trackers", func(ctx context.Context) workDone {
			update.trackers, update.err = source.trackers(ctx, target.hash)
			return workDone{detailTabUpdate: update, err: update.err}
		})
	case detailTabFiles:
		source, ok := backend.(torrentFilesService)
		if !ok {
			d.status = "File details are unavailable for this connection"
			return nil
		}
		filesSource := source
		return m.startWork(detailTabWork, "Loading torrent files", func(ctx context.Context) workDone {
			update.files, update.err = filesSource.files(ctx, target.hash)
			return workDone{detailTabUpdate: update, err: update.err}
		})
	case detailTabPeers:
		source, ok := backend.(torrentPeersService)
		if !ok {
			d.status = "Peer details are unavailable for this connection"
			return nil
		}
		peersSource := source
		rid := state.peers.RID
		return m.startWork(detailTabWork, "Loading torrent peers", func(ctx context.Context) workDone {
			update.peers, update.err = peersSource.peers(ctx, target.hash, rid)
			return workDone{detailTabUpdate: update, err: update.err}
		})
	default:
		return nil
	}
}

func (m *model) loadPendingDetailTab() tea.Cmd {
	d := m.dialog
	if d == nil || d.detail == nil || d.detail.pendingTab == nil {
		return nil
	}
	return m.loadDetailTab(*d.detail.pendingTab)
}

func (m *model) setDetailFilePriority(state *detailDialogState, files []client.TorrentFile, priority client.FilePriority) tea.Cmd {
	if m.busy != "" {
		m.dialog.status = "Finish the current request first"
		return nil
	}
	if len(files) == 0 {
		return nil
	}
	ids := make([]int, 0, len(state.fileMarks))
	for _, file := range files {
		if state.fileMarks[file.Index] {
			ids = append(ids, file.Index)
		}
	}
	if len(ids) == 0 && state.fileCursor < len(files) {
		ids = []int{files[state.fileCursor].Index}
	}
	backend := m.backend
	service, ok := backend.(torrentFilePriorityService)
	if !ok {
		m.dialog.status = "File priority changes are unavailable for this connection"
		return nil
	}
	d, target := m.dialog, *m.dialog.torrentDetails
	filesSource, hasFilesSource := backend.(torrentFilesService)
	return m.startWork(detailTabWork, "Saving file priority", func(ctx context.Context) workDone {
		update := &detailTabUpdate{dialog: d, tab: detailTabFiles}
		if err := service.setFilePriority(ctx, target.hash, ids, priority); err != nil {
			update.err = err
			return workDone{detailTabUpdate: update, err: err}
		}
		if hasFilesSource {
			update.files, update.err = filesSource.files(ctx, target.hash)
		}
		return workDone{detailTabUpdate: update, err: update.err}
	})
}

func (m *model) finishDetailTabWork(done workDone) {
	update := done.detailTabUpdate
	if update == nil || m.dialog != update.dialog || m.dialog.torrentDetails == nil {
		return
	}
	if done.err != nil || update.err != nil {
		err := done.err
		if err == nil {
			err = update.err
		}
		m.dialog.status = "Details: " + err.Error()
		return
	}
	state := m.detailState(m.dialog)
	if update.tab == detailTabOverview {
		m.dialog.rows = done.rows
	} else if update.tab == detailTabFiles {
		state.files = append([]client.TorrentFile(nil), update.files...)
		for index := range state.fileMarks {
			found := false
			for _, file := range state.files {
				if file.Index == index {
					found = true
					break
				}
			}
			if !found {
				delete(state.fileMarks, index)
			}
		}
	} else if update.tab == detailTabPeers {
		state.peers.Merge(update.peers)
	} else if update.tab == detailTabTrackers {
		m.replaceTrackerEntries(update.trackers)
	}
	m.dialog.status = ""
}
