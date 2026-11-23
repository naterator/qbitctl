//go:build !plan9

package tui

import (
	"context"
	"errors"
	"fmt"
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

func (s *trackerDialogService) details(context.Context, string, bool) ([]detailRow, error) {
	return []detailRow{{label: "Comment", value: "Torrent overview metadata"}}, nil
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
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	details := m.dialog
	if details == nil || details.detail.tab != detailTabOverview {
		t.Fatalf("Enter did not open torrent overview: %#v", details)
	}
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: '2'}))
	if m.dialog != details || m.dialog.kind != dialogDetails || m.dialog.detail.tab != detailTabTrackers {
		t.Fatalf("dialog = %#v, want Trackers tab in torrent details", m.dialog)
	}
	frame := ansi.Strip(m.dialogView(100, 30))
	lines := strings.Split(frame, "\n")
	for i, line := range lines[:len(lines)-1] {
		if strings.Contains(line, "2 Trackers") {
			if strings.TrimSpace(lines[i+1]) != "" {
				t.Fatalf("torrent tabs have no blank line below them: %q", frame)
			}
			break
		}
	}
	if !containsAll(frame, "1 Overview", "2 Trackers", "3 Files", "4 Peers", "tab tabs") {
		t.Fatalf("detail tab strip is incomplete: %q", frame)
	}
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

func TestTrackerTabRefreshPreservesAnOpenEditForm(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	m.update(tea.KeyPressMsg{Code: 'e'})
	form := m.dialog
	request := form.request
	draft := "https://replacement.example/announce"
	form.fields[0].input.SetValue(draft)
	backend.torrents[0].Name = "Renamed fixture"
	backend.entries[0].Status = 4

	finishTrackerDialogWork(t, m, m.refreshTorrentDetails())
	if m.dialog != form || form.kind != dialogTrackerEdit || form.title != "Edit tracker" ||
		form.value(0) != draft || form.request.original != request.original || form.request.hashes != request.hashes {
		t.Fatalf("refresh changed an open tracker edit: %#v", form)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if form.kind != dialogDetails || form.title != "Renamed fixture" || form.trackerEntries[form.selected].Status != 4 {
		t.Fatalf("returning from the form did not show refreshed torrent and tracker data: %#v", form)
	}
}

func TestTrackerTabKeepsTabsVisibleWhileScrolling(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	m.width, m.height = 80, 24
	for i := range 30 {
		backend.entries = append(backend.entries, client.TrackerEntry{URL: fmt.Sprintf("https://tracker-%d.example/announce", i)})
	}
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: 't'}))
	m.update(tea.KeyPressMsg{Code: tea.KeyEnd})
	frame := ansi.Strip(m.dialogView(80, 24))
	if !containsAll(frame, "2 Trackers", "tracker-29.example") || m.dialog.scroll == 0 {
		t.Fatalf("scrolled Trackers tab lost the tab bar or selection: %q", frame)
	}
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: '1'}))
	if frame := ansi.Strip(m.dialogView(80, 24)); !strings.Contains(frame, "Torrent overview metadata") {
		t.Fatalf("loading Overview after using the Trackers shortcut did not show metadata: %q", frame)
	}
}

func TestTrackerTabNavigationKeepsDiscoveryEntriesReadOnly(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	m.update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.dialog.selected != 1 {
		t.Fatalf("up from the first tracker selected %d, want the last tracker", m.dialog.selected)
	}
	m.update(tea.KeyPressMsg{Code: 'e'})
	if m.dialog.kind != dialogDetails || !strings.Contains(m.dialog.status, "cannot be edited") {
		t.Fatal("the discovery entry was editable")
	}
	if cmd := m.update(tea.KeyPressMsg{Code: 'd'}); cmd != nil || !strings.Contains(m.dialog.status, "cannot be removed") {
		t.Fatal("the discovery entry was removable")
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.dialog.selected != 0 || len(backend.recordedActions()) != 0 {
		t.Fatal("down did not wrap around or a protected entry was modified")
	}
}

func TestTrackerFormCanCorrectBlankAndRejectedURLs(t *testing.T) {
	m, backend := newTrackerDialogModel(t)
	finishTrackerDialogWork(t, m, m.loadDetails(true))
	m.update(tea.KeyPressMsg{Code: 'a'})
	form := m.dialog
	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || form.status != "Tracker URL is required" {
		t.Fatal("blank tracker URL was accepted")
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if form.value(0) != "h" {
		t.Fatalf("tracker input = %q, want typed text", form.value(0))
	}
	backend.actionErr = errors.New("tracker rejected by server")
	url := "https://example.net/announce"
	form.fields[0].input.SetValue(url)
	finishTrackerDialogWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.dialog != form || form.kind != dialogTrackerAdd || form.value(0) != url || form.status != backend.actionErr.Error() {
		t.Fatal("the tracker form did not keep the input and server error")
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
	if m.dialog.kind != dialogDetails || m.dialog.detail.tab != detailTabTrackers || m.dialog.selected != 1 || len(m.dialog.trackerEntries) != 2 {
		t.Fatalf("after add escape dialog = %#v, want list with selection and rows", m.dialog)
	}
	m.dialog.selected = 0
	m.update(tea.KeyPressMsg{Code: 'e'})
	if m.dialog.kind != dialogTrackerEdit {
		t.Fatalf("edit dialog kind = %q", m.dialog.kind)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog.kind != dialogDetails || m.dialog.detail.tab != detailTabTrackers || m.dialog.selected != 0 || len(m.dialog.trackerEntries) != 2 {
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
