//go:build !plan9

package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func queueTestTorrents() []client.TorrentInfo {
	return []client.TorrentInfo{
		{Hash: "z", Name: "Zulu", State: "pausedUP", Progress: .90},
		{Hash: "a", Name: "alpha", State: "Downloading", Progress: .25, DLSpeed: 10},
		{Hash: "b", Name: "Bravo", State: "queuedDL", Progress: .25},
		{Hash: "c", Name: "charlie", State: "downloading", Progress: .75, UpSpeed: 10},
		{Hash: "d", Name: "Delta", State: "stalledDL", Progress: .25},
	}
}

func queueTestHashes(m *model) []string {
	got := make([]string, 0, len(m.queue))
	for _, torrent := range m.queue {
		got = append(got, torrent.Hash)
	}
	return got
}

func TestQueueSortsByVisibleColumns(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.replaceQueue(queueTestTorrents())

	for _, test := range []struct {
		order queueSort
		want  []string
	}{
		{queueActiveFirst, []string{"a", "c", "b", "d", "z"}},
		{queueNameAsc, []string{"a", "b", "c", "d", "z"}},
		{queueNameDesc, []string{"z", "d", "c", "b", "a"}},
		{queueProgressAsc, []string{"a", "b", "d", "c", "z"}},
		{queueProgressDesc, []string{"z", "c", "a", "b", "d"}},
		{queueStateAsc, []string{"a", "c", "z", "b", "d"}},
		{queueStateDesc, []string{"d", "b", "z", "a", "c"}},
	} {
		m.setQueueSort(test.order)
		if got := queueTestHashes(m); !slices.Equal(got, test.want) {
			t.Errorf("sort %q = %v, want %v", test.order, got, test.want)
		}
	}
}

func TestQueueSortPreservesFocusAndMarksOnRefresh(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.replaceQueue([]client.TorrentInfo{
		{Hash: "z", Name: "Zulu", Progress: .90},
		{Hash: "a", Name: "alpha", Progress: .20},
		{Hash: "b", Name: "Bravo", Progress: .50},
	})
	// Active-first falls back to the displayed name, so move from alpha to Bravo.
	m.moveCursor(1)
	m.toggleMark()
	m.setQueueSort(queueProgressDesc)
	if torrent, ok := m.focusedTorrent(); !ok || torrent.Hash != "b" || m.queueIndex != 1 {
		t.Fatalf("sort moved focus: queue=%v focus=%d", queueTestHashes(m), m.queueIndex)
	}

	// A refresh changes the order, but focus and the mark follow the torrent hash.
	m.replaceQueue([]client.TorrentInfo{
		{Hash: "a", Name: "alpha", Progress: .40},
		{Hash: "b", Name: "Bravo", Progress: .95},
		{Hash: "z", Name: "Zulu", Progress: .10},
	})
	if torrent, ok := m.focusedTorrent(); !ok || torrent.Hash != "b" || m.queueIndex != 0 {
		t.Fatalf("refresh moved focus: queue=%v focus=%d", queueTestHashes(m), m.queueIndex)
	}
	if got := m.targetHashes(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("marked hashes = %v, want [b]", got)
	}
}

func TestQueueSortsBySizeRatioETAAndRates(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.replaceQueue([]client.TorrentInfo{
		{Hash: "a", Name: "Alpha", TotalSize: 1000, Ratio: 1.25, ETA: 20, DLSpeed: 1, UpSpeed: 30},
		{Hash: "b", Name: "Bravo", TotalSize: 10, Ratio: -1, ETA: -1, DLSpeed: 40, UpSpeed: 10},
		{Hash: "c", Name: "Charlie", TotalSize: 100, Ratio: 2, ETA: 10, DLSpeed: 20, UpSpeed: 10},
	})
	for _, tc := range []struct {
		order queueSort
		want  []string
	}{
		{queueSizeDesc, []string{"a", "c", "b"}},
		{queueRatioDesc, []string{"b", "c", "a"}},
		{queueETAAsc, []string{"c", "a", "b"}},
		{queueDownloadDesc, []string{"b", "c", "a"}},
		{queueUploadDesc, []string{"a", "b", "c"}},
	} {
		m.setQueueSort(tc.order)
		if got := queueTestHashes(m); !slices.Equal(got, tc.want) {
			t.Errorf("sort %q = %v, want %v", tc.order, got, tc.want)
		}
	}
}

func TestQueueDetailShowsKnownSwarmCounts(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.replaceQueue([]client.TorrentInfo{{
		Hash: "a", Name: "Alpha", ConnectedSeeds: new(int64(0)), TotalSeeders: new(int64(17)),
		ConnectedLeechers: new(int64(2)), TotalLeechers: new(int64(-1)), QueuePosition: new(int64(3)),
	}})
	display := strings.Join(m.viewTorrentDetail(80), "\n")
	for _, want := range []string{"0 connected · 17 in swarm", "2 connected", "Position"} {
		if !strings.Contains(display, want) {
			t.Errorf("missing %q in detail %q", want, display)
		}
	}
	if strings.Contains(display, "-1 in swarm") {
		t.Errorf("detail shows unknown count: %q", display)
	}
}

func TestQueueSortPickerAndSearchSortAreIndependent(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.handleKey("ctrl+p")
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatalf("palette shortcut opened %+v", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("sort queue")
	m.dialog.refreshPalette()
	if len(m.dialog.items) != 1 || m.dialog.items[0].action != "sort-queue" {
		t.Fatalf("filtered palette items = %+v", m.dialog.items)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.kind != dialogQueueSort {
		t.Fatalf("sort queue palette action opened %+v", m.dialog)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// The direct shortcut and picker labels expose the Queue columns, in order.
	m.handleKey("o")
	if m.dialog == nil || m.dialog.kind != dialogQueueSort {
		t.Fatalf("sort queue shortcut opened %+v", m.dialog)
	}
	wantLabels := []string{
		"Active first", "Name A-Z", "Name Z-A", "Progress low to high",
		"Progress high to low", "State A-Z", "State Z-A",
		"Largest first", "Highest ratio", "Shortest ETA", "Highest download speed", "Highest upload speed",
	}
	gotLabels := make([]string, 0, len(m.dialog.items))
	for _, item := range m.dialog.items {
		gotLabels = append(gotLabels, item.label)
	}
	if !slices.Equal(gotLabels, wantLabels) {
		t.Fatalf("queue sort labels = %v, want %v", gotLabels, wantLabels)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.queueSort != queueNameAsc || m.resultSort != resultOriginal {
		t.Fatalf("applying queue sort set queue to %q and search to %q", m.queueSort, m.resultSort)
	}

	m.page = searchPage
	m.handleKey("s")
	if m.dialog == nil || m.dialog.kind != dialogResultSort {
		t.Fatalf("search sort shortcut opened %+v", m.dialog)
	}
	view := m.dialogView(80, 24)
	if !strings.Contains(view, "Search order") || !strings.Contains(view, "Most seeders") {
		t.Fatalf("search picker did not show its sort choices")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resultSort != resultSeedersDesc || m.queueSort != queueNameAsc {
		t.Fatalf("search sort set search to %q and queue to %q", m.resultSort, m.queueSort)
	}
}
