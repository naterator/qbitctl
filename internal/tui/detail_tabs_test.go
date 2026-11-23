//go:build !plan9

package tui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

type detailTabsService struct {
	*liveDetailsService
	mu             sync.Mutex
	fileRows       []client.TorrentFile
	peerResponses  []client.TorrentPeers
	peerRIDs       []int64
	priorityIDs    []int
	priorityValues []client.FilePriority
}

func (s *detailTabsService) files(context.Context, string) ([]client.TorrentFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]client.TorrentFile(nil), s.fileRows...), nil
}

func (s *detailTabsService) peers(_ context.Context, _ string, rid int64) (client.TorrentPeers, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerRIDs = append(s.peerRIDs, rid)
	if len(s.peerResponses) == 0 {
		return client.TorrentPeers{}, nil
	}
	response := s.peerResponses[0]
	s.peerResponses = s.peerResponses[1:]
	return response, nil
}

func (s *detailTabsService) setFilePriority(_ context.Context, _ string, ids []int, priority client.FilePriority) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.priorityIDs = append([]int(nil), ids...)
	s.priorityValues = append(s.priorityValues, priority)
	for i := range s.fileRows {
		for _, id := range ids {
			if s.fileRows[i].Index == id {
				s.fileRows[i].Priority = int(priority)
			}
		}
	}
	return nil
}

func TestDetailTabSwitchWaitsForActiveRefresh(t *testing.T) {
	torrent := liveDetailsTorrent("Example", "hash", 0.5)
	backend := &detailTabsService{
		liveDetailsService: &liveDetailsService{
			rows:     []detailRow{{label: "Name", value: torrent.Name}},
			torrents: []client.TorrentInfo{torrent},
		},
		peerResponses: []client.TorrentPeers{{
			RID: 1, FullUpdate: true, Peers: map[string]client.TorrentPeer{"peer": {Client: "Fixture"}},
		}},
	}
	m := newLiveDetailsModel(t, backend.liveDetailsService, torrent)
	m.backend = backend
	finishLiveDetailsCommand(t, m, m.loadDetails(false))

	refresh := m.refreshTorrentDetails()
	if cmd := m.update(tea.KeyPressMsg{Code: '3'}); cmd != nil || m.dialog.detail.tab != detailTabOverview {
		t.Fatal("peer tab changed before the current refresh completed")
	}
	finishLiveDetailsCommand(t, m, m.finishWork(refresh().(workDone)))
	if m.dialog.detail.tab != detailTabPeers || m.dialog.detail.peers.Peers["peer"].Client != "Fixture" {
		t.Fatal("the queued peer tab request did not load when the refresh finished")
	}
}

func TestDetailFilesTabLoadsSearchesSelectsAndChangesPriority(t *testing.T) {
	torrent := liveDetailsTorrent("Example", "hash", 0.5)
	backend := &detailTabsService{
		liveDetailsService: &liveDetailsService{rows: []detailRow{{label: "Name", value: torrent.Name}}, torrents: []client.TorrentInfo{torrent}},
		fileRows: []client.TorrentFile{
			{Index: 0, Name: "folder/alpha.mkv", Size: 1024, Progress: 1, Priority: int(client.FilePriorityNormal)},
			{Index: 1, Name: "folder/beta.srt", Size: 512, Progress: 0.5, Priority: int(client.FilePriorityHigh)},
		},
	}
	m := newLiveDetailsModel(t, backend.liveDetailsService, torrent)
	// Use the richer service for optional tab endpoints after constructing the
	// model through the existing live-details harness.
	m.backend = backend
	finishLiveDetailsCommand(t, m, m.loadDetails(false))

	cmd := m.update(tea.KeyPressMsg{Code: '2'})
	done, ok := cmd().(workDone)
	if !ok {
		t.Fatalf("files tab command returned %T", cmd())
	}
	m.finishWork(done)
	if m.dialog.detail.tab != detailTabFiles || len(m.dialog.detail.files) != 2 {
		t.Fatalf("files tab state = %#v", m.dialog.detail)
	}
	if frame := ansi.Strip(m.dialogView(100, 30)); !containsAll(frame, "folder/alpha.mkv", "folder/beta.srt", "2 Files") {
		t.Fatalf("files dialog = %q", frame)
	}

	// Search is entered with '/', then only single-rune key events become the
	// filter. The selected file is the sole target after filtering.
	m.update(tea.KeyPressMsg{Code: '/'})
	m.update(tea.KeyPressMsg{Code: 'b'})
	m.update(tea.KeyPressMsg{Code: 'e'})
	if got := filteredDetailFiles(m.dialog.detail.files, m.dialog.detail.fileFilter); len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("filtered files = %#v", got)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.update(tea.KeyPressMsg{Code: ' '})
	cmd = m.update(tea.KeyPressMsg{Code: '6'})
	if cmd == nil {
		t.Fatalf("priority key returned nil; tab=%v filter=%q active=%v marks=%v busy=%q status=%q", m.dialog.detail.tab, m.dialog.detail.fileFilter, m.dialog.detail.fileFilterActive, m.dialog.detail.fileMarks, m.busy, m.dialog.status)
	}
	done = cmd().(workDone)
	m.finishWork(done)
	backend.mu.Lock()
	ids := append([]int(nil), backend.priorityIDs...)
	priorities := append([]client.FilePriority(nil), backend.priorityValues...)
	backend.mu.Unlock()
	if !slices.Equal(ids, []int{1}) || len(priorities) != 1 || priorities[0] != client.FilePriorityHigh {
		t.Fatalf("priority request ids=%v priorities=%v", ids, priorities)
	}
}

func TestDetailPeersTabMergesPartialUpdatesAndRetainsSelection(t *testing.T) {
	torrent := liveDetailsTorrent("Example", "hash", 0.5)
	var full, partial client.TorrentPeers
	if err := json.Unmarshal([]byte(`{"rid":1,"full_update":true,"show_flags":true,"peers":{"peer":{"client":"Client A","ip":"2001:db8::1","port":6881,"dl_speed":10,"files":"one.mkv"}}}`), &full); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"rid":2,"full_update":false,"peers":{"peer":{"dl_speed":0,"files":""}}}`), &partial); err != nil {
		t.Fatal(err)
	}
	backend := &detailTabsService{
		liveDetailsService: &liveDetailsService{rows: []detailRow{{label: "Name", value: torrent.Name}}, torrents: []client.TorrentInfo{torrent}},
		peerResponses:      []client.TorrentPeers{full, partial},
	}
	m := newLiveDetailsModel(t, backend.liveDetailsService, torrent)
	m.backend = backend
	finishLiveDetailsCommand(t, m, m.loadDetails(false))
	cmd := m.update(tea.KeyPressMsg{Code: '3'})
	done := cmd().(workDone)
	m.finishWork(done)
	if m.dialog.detail.peers.Peers["peer"].Client != "Client A" {
		t.Fatalf("initial peer response = %#v", m.dialog.detail.peers)
	}
	if got := backend.peerRIDs; !slices.Equal(got, []int64{0}) {
		t.Fatalf("initial peer RID calls = %v", got)
	}

	finishLiveDetailsCommand(t, m, m.refreshTorrentDetails())
	peer := m.dialog.detail.peers.Peers["peer"]
	if peer.Client != "Client A" || peer.IP != "2001:db8::1" || peer.DLSpeed != 0 || peer.Files != "" {
		t.Fatalf("partial peer refresh discarded fields or zero updates: %#v", peer)
	}
	if got := backend.peerRIDs; !slices.Equal(got, []int64{0, 1}) {
		t.Fatalf("peer RID calls = %v", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
