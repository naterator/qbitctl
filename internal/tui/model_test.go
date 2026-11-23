//go:build !plan9

package tui

import (
	"context"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestQueueRefreshKeepsFocusedTorrentAndSelection(t *testing.T) {
	m := newModel(t.Context(), nil)
	alpha := client.TorrentInfo{Hash: "a", Name: "Alpha", State: "stoppedDL"}
	beta := client.TorrentInfo{Hash: "b", Name: "Beta", State: "downloading", DLSpeed: 10}
	m.replaceQueue([]client.TorrentInfo{alpha, beta})
	m.moveCursor(1)
	m.toggleMark()
	m.moveCursor(-1)
	m.toggleMark()

	alpha.UpSpeed, alpha.State, beta.DLSpeed = 20, "uploading", 0
	m.replaceQueue([]client.TorrentInfo{beta, alpha})
	focused, ok := m.focusedTorrent()
	if !ok || focused.Hash != beta.Hash {
		t.Fatalf("refresh moved focus to %+v", focused)
	}
	if got := m.targetHashes(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("marked hashes = %v", got)
	}
	m.setFilter("alpha", "seeding")
	if got := m.targetHashes(); !slices.Equal(got, []string{"a"}) || len(m.marked) != 0 {
		t.Fatalf("filter left hidden selections: %v, marks %v", got, m.marked)
	}
}

func TestRefreshIntervalAdaptsToQueueActivity(t *testing.T) {
	m := newModel(t.Context(), nil)
	if got := m.refreshInterval(); got != activeRefreshInterval {
		t.Fatalf("initial refresh interval = %s, want %s", got, activeRefreshInterval)
	}
	m.replaceQueue([]client.TorrentInfo{{Hash: "idle", State: "stoppedDL"}})
	if got := m.refreshInterval(); got != idleRefreshInterval {
		t.Fatalf("idle refresh interval = %s, want %s", got, idleRefreshInterval)
	}
	m.replaceQueue([]client.TorrentInfo{{Hash: "active", State: "downloading", DLSpeed: 1}})
	if got := m.refreshInterval(); got != activeRefreshInterval {
		t.Fatalf("active refresh interval = %s, want %s", got, activeRefreshInterval)
	}
	m.replaceQueue(nil)
	if got := m.refreshInterval(); got != idleRefreshInterval {
		t.Fatalf("empty queue refresh interval = %s, want %s", got, idleRefreshInterval)
	}
}

func TestRefreshIntervalTracksServerStateWithoutQueueRebuild(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.replaceQueue([]client.TorrentInfo{{Hash: "idle", State: "stoppedDL"}})
	if got := m.refreshInterval(); got != idleRefreshInterval {
		t.Fatalf("initial idle refresh interval = %s, want %s", got, idleRefreshInterval)
	}
	download, upload := int64(42), int64(0)
	m.updateQueueActivity(queueTransferState{download: &download, upload: &upload})
	if got := m.refreshInterval(); got != activeRefreshInterval {
		t.Fatalf("server-state active refresh interval = %s, want %s", got, activeRefreshInterval)
	}
	download = 0
	m.updateQueueActivity(queueTransferState{download: &download, upload: &upload})
	if got := m.refreshInterval(); got != idleRefreshInterval {
		t.Fatalf("server-state idle refresh interval = %s, want %s", got, idleRefreshInterval)
	}
	m.queueActive = true
	m.updateQueueActivity(queueTransferState{})
	if got := m.refreshInterval(); got != activeRefreshInterval {
		t.Fatalf("missing server-state rates changed refresh interval = %s, want %s", got, activeRefreshInterval)
	}
}

func TestNoOpQueueWorkSkipsQueueRebuild(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = queuePage
	initial := []client.TorrentInfo{{Hash: "a", Name: "Alpha", State: "downloading"}}
	m.replaceQueue(initial)
	queueBacking := &m.queue[0]
	allBacking := &m.allTorrents[0]
	m.finishWork(workDone{
		kind:         queueWork,
		queue:        []client.TorrentInfo{{Hash: "a", Name: "Alpha", State: "downloading"}},
		queueChanged: false,
	})
	if &m.queue[0] != queueBacking || &m.allTorrents[0] != allBacking {
		t.Fatal("no-op queue work rebuilt queue slices")
	}
}

type searchOnlyService struct {
	service
	started chan struct{}
	cleaned chan struct{}
}

func TestPauseStopsAutomaticRefreshAndPreservesManualRefresh(t *testing.T) {
	backend := &recordingService{}
	m := newModel(t.Context(), backend)
	m.openAction("toggle-refresh")
	m.update(refreshTick{})
	if !m.refreshPaused || m.busy != "" || backend.queueCallCount() != 0 {
		t.Fatal("an automatic refresh started while refresh was paused")
	}
	runTUIWork(t, m, m.handleKey("r"))
	if backend.queueCallCount() != 1 {
		t.Fatal("manual refresh did not run while automatic refresh was paused")
	}
	runTUIWork(t, m, m.openAction("toggle-refresh"))
	if m.refreshPaused || backend.queueCallCount() != 2 {
		t.Fatal("resuming automatic refresh did not immediately refresh the queue")
	}
}

func (s searchOnlyService) search(ctx context.Context, _ string, _ string, _ time.Duration) ([]client.SearchResult, string, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cleaned)
	return nil, "", ctx.Err()
}

func TestQuitWaitsForCanceledSearch(t *testing.T) {
	backend := searchOnlyService{started: make(chan struct{}), cleaned: make(chan struct{})}
	m := newModel(t.Context(), backend)
	finishSearch := m.launchSearch("Ubuntu", "jackett", 30*time.Second)
	<-backend.started
	if cmd := m.requestQuit(); cmd != nil {
		t.Fatal("quit returned before the search worker finished")
	}
	finished := finishSearch().(workDone)
	<-backend.cleaned
	quit := m.finishWork(finished)
	if quit == nil {
		t.Fatal("quit did not resume after cleanup")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("cleanup did not finish with a quit message")
	}
	m.workers.Wait()
}
