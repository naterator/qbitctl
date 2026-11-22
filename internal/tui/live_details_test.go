//go:build !plan9

package tui

import (
	"context"
	"slices"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type liveDetailsCall struct {
	hash     string
	trackers bool
}

// liveDetailsService keeps the live-details tests deterministic without
// depending on the HTTP adapter. The queue request can be held open so a
// dialog close can be interleaved with a refresh response.
type liveDetailsService struct {
	service

	mu           sync.Mutex
	torrents     []client.TorrentInfo
	queueErr     error
	rows         []detailRow
	detailsErr   error
	detailCalls  []liveDetailsCall
	queueCalls   int
	queueStarted chan struct{}
	queueRelease chan struct{}
	queueStart   sync.Once
}

func (s *liveDetailsService) queue(ctx context.Context) ([]client.TorrentInfo, error) {
	s.queueStart.Do(func() {
		if s.queueStarted != nil {
			close(s.queueStarted)
		}
	})
	if s.queueRelease != nil {
		select {
		case <-s.queueRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueCalls++
	return append([]client.TorrentInfo(nil), s.torrents...), s.queueErr
}

func (s *liveDetailsService) details(_ context.Context, hash string, trackers bool) ([]detailRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detailCalls = append(s.detailCalls, liveDetailsCall{hash: hash, trackers: trackers})
	return append([]detailRow(nil), s.rows...), s.detailsErr
}

func (s *liveDetailsService) setQueue(torrents []client.TorrentInfo, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.torrents, s.queueErr = append([]client.TorrentInfo(nil), torrents...), err
}

func (s *liveDetailsService) setDetails(rows []detailRow, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows, s.detailsErr = append([]detailRow(nil), rows...), err
}

func (s *liveDetailsService) calls() []liveDetailsCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]liveDetailsCall(nil), s.detailCalls...)
}

func (s *liveDetailsService) queueCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCalls
}

func liveDetailsTorrent(name, hash string, progress float64) client.TorrentInfo {
	return client.TorrentInfo{Name: name, Hash: hash, Progress: progress, State: "downloading"}
}

func newLiveDetailsModel(t *testing.T, backend *liveDetailsService, torrents ...client.TorrentInfo) *model {
	t.Helper()
	m := newModel(t.Context(), backend)
	m.page = queuePage
	// A progress sort makes the reorder assertion explicit and deterministic.
	m.queueSort = queueProgressDesc
	m.replaceQueue(torrents)
	return m
}

func finishLiveDetailsCommand(t *testing.T, m *model, command tea.Cmd) workDone {
	t.Helper()
	if command == nil {
		t.Fatal("live-details command was nil")
	}
	message := command()
	done, ok := message.(workDone)
	if !ok {
		t.Fatalf("live-details command returned %T, want workDone", message)
	}
	m.finishWork(done)
	m.workers.Wait()
	return done
}

func detailRowValue(rows []detailRow, label string) (string, bool) {
	for _, row := range rows {
		if row.label == label {
			return row.value, true
		}
	}
	return "", false
}

func TestLoadDetailsBindsDialogToFocusedTorrent(t *testing.T) {
	original := liveDetailsTorrent("Original", "original", 0.40)
	other := liveDetailsTorrent("Other", "other", 0.20)
	rows := []detailRow{{label: "Name", value: original.Name}, {label: "Progress", value: "40%"}}
	backend := &liveDetailsService{rows: rows}
	m := newLiveDetailsModel(t, backend, original, other)

	finishLiveDetailsCommand(t, m, m.loadDetails(false))

	calls := backend.calls()
	if len(calls) != 1 || calls[0] != (liveDetailsCall{hash: original.Hash}) {
		t.Fatalf("details calls = %#v, want one request for %q", calls, original.Hash)
	}
	if m.dialog == nil || m.dialog.kind != dialogDetails {
		t.Fatalf("dialog = %#v, want torrent details dialog", m.dialog)
	}
	if m.dialog.torrentDetails == nil || m.dialog.torrentDetails.hash != original.Hash {
		t.Fatalf("dialog target = %#v, want hash %q", m.dialog.torrentDetails, original.Hash)
	}
	if m.dialog.title != original.Name {
		t.Fatalf("dialog title = %q, want %q", m.dialog.title, original.Name)
	}
	if !slices.Equal(m.dialog.rows, rows) {
		t.Fatalf("dialog rows = %#v, want %#v", m.dialog.rows, rows)
	}
	if got, ok := detailRowValue(m.dialog.rows, "Progress"); !ok || got != "40%" {
		t.Fatalf("dialog progress = %q (present=%v), want 40%%", got, ok)
	}
}

func TestRefreshTorrentDetailsUpdatesQueueAndModalByHash(t *testing.T) {
	original := liveDetailsTorrent("Original", "original", 0.40)
	other := liveDetailsTorrent("Other", "other", 0.20)
	backend := &liveDetailsService{
		rows: []detailRow{{label: "Name", value: original.Name}, {label: "Progress", value: "40%"}},
	}
	m := newLiveDetailsModel(t, backend, original, other)
	finishLiveDetailsCommand(t, m, m.loadDetails(false))
	modal := m.dialog
	modal.scroll = 3

	refreshedOriginal := liveDetailsTorrent("Original (renamed)", original.Hash, 0.80)
	refreshedOther := liveDetailsTorrent(other.Name, other.Hash, 0.90)
	backend.setQueue([]client.TorrentInfo{refreshedOriginal, refreshedOther}, nil)
	updatedRows := []detailRow{{label: "Name", value: refreshedOriginal.Name}, {label: "Progress", value: "80%"}}
	backend.setDetails(updatedRows, nil)

	done := finishLiveDetailsCommand(t, m, m.refreshTorrentDetails())
	if done.kind != detailRefreshWork {
		t.Fatalf("refresh work kind = %v, want detail refresh", done.kind)
	}
	if m.dialog != modal {
		t.Fatal("refresh replaced the live details dialog")
	}
	if modal.torrentDetails == nil || modal.torrentDetails.hash != original.Hash {
		t.Fatalf("refreshed dialog target = %#v, want original hash", modal.torrentDetails)
	}
	if modal.scroll != 3 {
		t.Fatalf("dialog scroll = %d, want retained scroll 3", modal.scroll)
	}
	if modal.title != refreshedOriginal.Name {
		t.Fatalf("dialog title = %q, want %q", modal.title, refreshedOriginal.Name)
	}
	if !slices.Equal(modal.rows, updatedRows) {
		t.Fatalf("dialog rows = %#v, want %#v", modal.rows, updatedRows)
	}
	if got, ok := detailRowValue(modal.rows, "Progress"); !ok || got != "80%" {
		t.Fatalf("refreshed dialog progress = %q (present=%v), want 80%%", got, ok)
	}
	if len(m.queue) != 2 || m.queue[m.queueIndex].Hash != original.Hash {
		t.Fatalf("queue focus = index %d, queue %#v; want original torrent retained", m.queueIndex, m.queue)
	}
	if m.queueIndex != 1 {
		t.Fatalf("queue index = %d, want reordered original at index 1", m.queueIndex)
	}
	if m.queue[0].Hash != other.Hash || m.queue[0].Progress != 0.90 {
		t.Fatalf("queue background = %#v, want other at 90%% ahead of original", m.queue)
	}
	calls := backend.calls()
	if len(calls) != 2 || calls[1] != (liveDetailsCall{hash: original.Hash}) {
		t.Fatalf("details calls = %#v, want refresh for original hash", calls)
	}
}

func TestRefreshTorrentDetailsAppliesQueueWhenDialogCloses(t *testing.T) {
	original := liveDetailsTorrent("Original", "original", 0.40)
	other := liveDetailsTorrent("Other", "other", 0.20)
	backend := &liveDetailsService{
		rows:         []detailRow{{label: "Progress", value: "40%"}},
		queueStarted: make(chan struct{}),
		queueRelease: make(chan struct{}),
	}
	m := newLiveDetailsModel(t, backend, original, other)
	finishLiveDetailsCommand(t, m, m.loadDetails(false))
	backend.setQueue([]client.TorrentInfo{
		liveDetailsTorrent(original.Name, original.Hash, 0.80),
		liveDetailsTorrent(other.Name, other.Hash, 0.90),
	}, nil)

	refresh := m.refreshTorrentDetails()
	message := make(chan tea.Msg, 1)
	go func() { message <- refresh() }()
	select {
	case <-backend.queueStarted:
	case <-m.ctx.Done():
		t.Fatal("test context canceled while waiting for queue refresh")
	}
	if m.update(tea.KeyPressMsg{Code: tea.KeyEscape}) != nil {
		t.Fatal("closing live details dialog returned an unexpected command")
	}
	if m.dialog != nil {
		t.Fatal("Escape did not close the live details dialog")
	}
	close(backend.queueRelease)

	raw := <-message
	done, ok := raw.(workDone)
	if !ok {
		t.Fatalf("refresh command returned %T, want workDone", raw)
	}
	m.finishWork(done)
	m.workers.Wait()
	if m.dialog != nil {
		t.Fatal("closed details dialog was reopened by its late response")
	}
	if len(m.queue) != 2 || m.queue[m.queueIndex].Hash != original.Hash {
		t.Fatalf("queue after close = index %d, queue %#v; want original focus retained", m.queueIndex, m.queue)
	}
	if m.queue[m.queueIndex].Progress != 0.80 {
		t.Fatalf("queue progress after close = %v, want 0.80", m.queue[m.queueIndex].Progress)
	}
}

func TestRefreshTickRefreshesOnlyTorrentDetails(t *testing.T) {
	original := liveDetailsTorrent("Original", "original", 0.40)
	other := liveDetailsTorrent("Other", "other", 0.20)
	backend := &liveDetailsService{
		rows:     []detailRow{{label: "Progress", value: "40%"}},
		torrents: []client.TorrentInfo{liveDetailsTorrent(original.Name, original.Hash, 0.80), liveDetailsTorrent(other.Name, other.Hash, 0.90)},
	}
	m := newLiveDetailsModel(t, backend, original, other)
	finishLiveDetailsCommand(t, m, m.loadDetails(false))
	m.dialog.rows = []detailRow{{label: "Progress", value: "40%"}}
	backend.setDetails([]detailRow{{label: "Progress", value: "80%"}}, nil)

	cmd := m.update(refreshTick{})
	if cmd == nil || m.work != detailRefreshWork {
		t.Fatalf("refresh tick command = %v, work = %v; want live detail refresh", cmd != nil, m.work)
	}
	raw := cmd()
	batch, ok := raw.(tea.BatchMsg)
	if !ok || len(batch) < 1 {
		t.Fatalf("refresh tick command returned %T, want batch with refresh work", raw)
	}
	// refreshTorrentDetails is the first command; the second command is the
	// five-second timer and must not be executed by a unit test.
	raw = batch[0]()
	done, ok := raw.(workDone)
	if !ok {
		t.Fatalf("refresh tick work returned %T, want workDone", raw)
	}
	m.finishWork(done)
	m.workers.Wait()
	if m.dialog == nil || m.dialog.torrentDetails == nil {
		t.Fatal("refresh tick lost the torrent details dialog")
	}
	if got, _ := detailRowValue(m.dialog.rows, "Progress"); got != "80%" {
		t.Fatalf("refresh tick detail progress = %q, want 80%%", got)
	}
	if backend.queueCallCount() != 1 {
		t.Fatalf("refresh tick queue calls = %d, want 1", backend.queueCallCount())
	}
}

func TestRefreshTickLeavesNonTorrentDialogsUnchanged(t *testing.T) {
	for _, kind := range []dialogKind{dialogDetails, dialogHelp, dialogEditValue, dialogConfirm} {
		t.Run(string(kind), func(t *testing.T) {
			backend := &liveDetailsService{}
			m := newModel(t.Context(), backend)
			m.page = queuePage
			modal := &dialog{kind: kind, title: "unchanged"}
			m.dialog = modal

			if cmd := m.update(refreshTick{}); cmd == nil {
				t.Fatal("refresh tick returned nil timer for non-torrent dialog")
			}
			if m.dialog != modal {
				t.Fatal("refresh tick replaced a non-torrent dialog")
			}
			if m.busy != "" {
				t.Fatalf("refresh tick started work for %s dialog: busy=%q", kind, m.busy)
			}
			if backend.queueCallCount() != 0 {
				t.Fatalf("refresh tick fetched queue for %s dialog", kind)
			}
		})
	}
}
