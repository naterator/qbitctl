//go:build !plan9

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

type trackerDialogService struct {
	*recordingService
	entries []client.TrackerEntry
}

func (s *trackerDialogService) trackers(context.Context, string) ([]client.TrackerEntry, error) {
	return append([]client.TrackerEntry(nil), s.entries...), nil
}

func newTrackerDialogModel(t *testing.T) (*model, *trackerDialogService) {
	t.Helper()
	hash := "0123456789abcdef0123456789abcdef01234567"
	backend := &trackerDialogService{
		recordingService: &recordingService{torrents: []client.TorrentInfo{{Hash: hash, Name: "Example"}}},
		entries: []client.TrackerEntry{
			{URL: "https://user:secret@tracker.example/announce?passkey=hidden", Status: 2},
			{URL: "** [DHT] **", Status: 0},
		},
	}
	m := newModel(t.Context(), backend)
	m.page = queuePage
	m.replaceQueue(backend.torrents)
	return m, backend
}

func finishTrackerDialogWork(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	runTUIWork(t, m, cmd)
	m.workers.Wait()
}

func TestTrackerDialogDefaultsToSafeHostAndStatusAndRevealsURLOnRequest(t *testing.T) {
	m, _ := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	if m.dialog == nil || m.dialog.kind != dialogTrackers {
		t.Fatalf("dialog = %#v, want tracker dialog", m.dialog)
	}
	frame := ansi.Strip(m.dialogView(100, 30))
	if !strings.Contains(frame, "tracker.example") || !strings.Contains(frame, "Working") {
		t.Fatalf("safe tracker frame = %q, want host and status", frame)
	}
	if strings.Contains(frame, "secret") || strings.Contains(frame, "passkey=hidden") {
		t.Fatalf("safe tracker frame exposed full URL: %q", frame)
	}
	m.update(tea.KeyPressMsg{Code: 'v'})
	frame = ansi.Strip(m.dialogView(100, 30))
	if !strings.Contains(frame, "passkey=hidden") {
		t.Fatalf("revealed tracker frame = %q, want full URL after v", frame)
	}
}

func TestTrackerDialogAddEditRemoveActionsUseExactURLs(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))

	m.update(tea.KeyPressMsg{Code: 'e'})
	if m.dialog == nil || m.dialog.kind != dialogTrackerEdit {
		t.Fatalf("edit dialog = %#v, want tracker edit dialog", m.dialog)
	}
	if got := m.dialog.value(0); got != backend.entries[0].URL {
		t.Fatalf("edit dialog value = %q, want exact full URL", got)
	}
	m.dialog.fields[0].input.SetValue("https://new.example/announce?token=new")
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))

	m.update(tea.KeyPressMsg{Code: 'a'})
	if m.dialog == nil || m.dialog.kind != dialogTrackerAdd {
		t.Fatalf("add dialog = %#v, want tracker add dialog", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("udp://two.example:6969/announce")
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))

	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: 'd'}))
	actions := backend.recordedActions()
	if len(actions) != 3 {
		t.Fatalf("tracker actions = %#v, want edit/add/remove", actions)
	}
	if actions[0].kind != "tracker-edit" || actions[0].original != backend.entries[0].URL || actions[0].value != "https://new.example/announce?token=new" {
		t.Fatalf("edit action = %#v", actions[0])
	}
	if actions[1].kind != "tracker-add" || actions[1].value != "udp://two.example:6969/announce" {
		t.Fatalf("add action = %#v", actions[1])
	}
	if actions[2].kind != "tracker-remove" || actions[2].value != backend.entries[0].URL {
		t.Fatalf("remove action = %#v", actions[2])
	}
}

func TestTrackerDialogRefreshUpdatesRowsInPlace(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	modal := m.dialog
	backend.entries = []client.TrackerEntry{{URL: "https://updated.example/announce", Status: 4}}
	finishTrackerDialogWork(t, m, m.refreshTorrentDetails())
	if m.dialog != modal || len(m.dialog.trackerEntries) != 1 || m.dialog.trackerEntries[0].URL != backend.entries[0].URL {
		t.Fatalf("refreshed tracker dialog = %#v, want updated entry in same dialog", m.dialog)
	}
}

func TestTrackerDialogEscapeReturnsToListWithoutLosingSelection(t *testing.T) {
	m, _ := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	m.dialog.selected = 1
	m.update(tea.KeyPressMsg{Code: 'a'})
	if m.dialog.kind != dialogTrackerAdd {
		t.Fatalf("add dialog kind = %q", m.dialog.kind)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog.kind != dialogTrackers || m.dialog.selected != 1 || len(m.dialog.trackerEntries) != 2 {
		t.Fatalf("after add escape dialog = %#v, want list with selection and rows", m.dialog)
	}
	m.dialog.selected = 0
	m.update(tea.KeyPressMsg{Code: 'e'})
	if m.dialog.kind != dialogTrackerEdit {
		t.Fatalf("edit dialog kind = %q", m.dialog.kind)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog.kind != dialogTrackers || m.dialog.selected != 0 || len(m.dialog.trackerEntries) != 2 {
		t.Fatalf("after edit escape dialog = %#v, want list with selection and rows", m.dialog)
	}
}

func TestTrackerDialogRefreshKeepsExactURLSelectionAndClampsRemoved(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	m.dialog.selected = 1
	m.dialog.scroll = 1
	selectedURL := m.dialog.trackerEntries[1].URL
	backend.entries = []client.TrackerEntry{
		{URL: "https://new.example/announce", Status: 1},
		{URL: selectedURL, Status: 2},
		{URL: "https://third.example/announce", Status: 4},
	}
	finishTrackerDialogWork(t, m, m.refreshTorrentDetails())
	if m.dialog.selected != 1 || m.dialog.trackerEntries[m.dialog.selected].URL != selectedURL {
		t.Fatalf("reordered refresh selection = %d/%q, want exact URL %q", m.dialog.selected, m.dialog.trackerEntries[m.dialog.selected].URL, selectedURL)
	}
	backend.entries = []client.TrackerEntry{{URL: "https://new.example/announce", Status: 1}}
	finishTrackerDialogWork(t, m, m.refreshTorrentDetails())
	if m.dialog.selected != 0 || m.dialog.scroll != 0 || len(m.dialog.trackerEntries) != 1 {
		t.Fatalf("removed selection = index %d scroll %d entries %d, want clamped list", m.dialog.selected, m.dialog.scroll, len(m.dialog.trackerEntries))
	}
}
