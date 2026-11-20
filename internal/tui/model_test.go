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

type searchOnlyService struct {
	service
	started chan struct{}
	cleaned chan struct{}
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
