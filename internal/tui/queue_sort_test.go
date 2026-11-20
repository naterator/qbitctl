//go:build !plan9

package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestQueueSortOptions(t *testing.T) {
	var torrents []client.TorrentInfo
	if err := json.Unmarshal([]byte(`[
		{"hash":"a","name":"Alpha","size":5,"total_size":300,"num_complete":5,"num_seeds":99},
		{"hash":"b","name":"Bravo","size":80,"total_size":100,"num_complete":20},
		{"hash":"c","name":"Charlie","total_size":200,"num_complete":20},
		{"hash":"d","name":"Delta","total_size":-1,"num_complete":-1},
		{"hash":"e","name":"Echo","total_size":0,"num_complete":0},
		{"hash":"z","name":"Zulu","total_size":150,"dlspeed":10}
	]`), &torrents); err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), nil)
	m.replaceQueue(torrents)

	for _, test := range []struct {
		order resultSort
		want  []string
	}{
		{resultSeedersDesc, []string{"b", "c", "a", "e", "z", "d"}},
		{resultSeedersAsc, []string{"e", "a", "b", "c", "z", "d"}},
		{resultSizeDesc, []string{"a", "c", "z", "b", "e", "d"}},
		{resultSizeAsc, []string{"e", "b", "z", "c", "a", "d"}},
		{resultOriginal, []string{"z", "a", "b", "c", "d", "e"}},
	} {
		m.setQueueSort(test.order)
		got := make([]string, 0, len(m.queue))
		for _, torrent := range m.queue {
			got = append(got, torrent.Hash)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("sort %q = %v, want %v", test.order, got, test.want)
		}
	}
}

func TestQueueSortPreservesSelectionAsQueueRefreshes(t *testing.T) {
	m := newModel(t.Context(), nil)
	alpha := client.TorrentInfo{Hash: "a", Name: "Alpha", TotalSize: 300}
	bravo := client.TorrentInfo{Hash: "b", Name: "Bravo", TotalSize: 100}
	m.replaceQueue([]client.TorrentInfo{bravo, alpha})
	m.moveCursor(1)
	m.toggleMark()

	m.setQueueSort(resultSizeAsc)
	if m.queue[0].Hash != "b" || m.queueIndex != 0 {
		t.Fatalf("smallest first moved focus or did not reorder: queue %v, focus %d", m.queue, m.queueIndex)
	}

	bravo.TotalSize = 400
	m.replaceQueue([]client.TorrentInfo{bravo, alpha})
	if m.queue[0].Hash != "a" || m.queueIndex != 1 {
		t.Fatalf("refresh moved focus or did not reapply sort: queue %v, focus %d", m.queue, m.queueIndex)
	}
	if got := m.targetHashes(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("marked hashes = %v, want [b]", got)
	}
}

func TestQueueSortPicker(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.handleKey("ctrl+p")
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

	m.handleKey("o")
	if m.dialog == nil || m.dialog.kind != dialogQueueSort {
		t.Fatalf("sort queue shortcut opened %+v", m.dialog)
	}
	view := m.dialogView(80, 24)
	for _, label := range []string{"› Active first", "Most seeders", "Fewest seeders", "Largest size", "Smallest size"} {
		if !strings.Contains(view, label) {
			t.Errorf("queue sort picker omitted %q", label)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.queueSort != resultSeedersDesc || m.resultSort != resultOriginal {
		t.Fatalf("applying queue sort set queue to %q and search to %q", m.queueSort, m.resultSort)
	}
	for _, text := range []string{"Most seeders", "SEEDS", "o sort"} {
		if !strings.Contains(m.View().Content, text) {
			t.Errorf("queue view omitted %q", text)
		}
	}
}
