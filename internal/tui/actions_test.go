//go:build !plan9

package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestRemoveUsesFocusedOrMarkedTargets(t *testing.T) {
	tests := []struct {
		name  string
		marks []int
		focus int
		want  string
	}{
		{name: "focused", focus: 1, want: "b"},
		{name: "marked in display order", marks: []int{0, 2}, focus: 1, want: "a|c"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &recordingService{}
			m := newModel(t.Context(), service)
			seedRecordingQueue(m, service,
				client.TorrentInfo{Hash: "a", Name: "Alpha"},
				client.TorrentInfo{Hash: "b", Name: "Beta"},
				client.TorrentInfo{Hash: "c", Name: "Gamma"},
			)
			m.queueIndex = test.focus
			for _, index := range test.marks {
				m.queueIndex = index
				m.toggleMark()
			}
			m.queueIndex = test.focus
			m.openRemoveDialog(false)
			if m.dialog == nil || m.dialog.request.hashes != test.want {
				t.Fatalf("remove dialog request = %#v, want %q", m.dialog, test.want)
			}
			runTUIWork(t, m, pressTUIKey(m, tea.KeyEnter))
			actions := service.recordedActions()
			if len(actions) != 1 || actions[0].kind != "remove" || actions[0].hashes != test.want {
				t.Fatalf("actions = %#v, want remove %q", actions, test.want)
			}
			if service.queueCallCount() != 1 {
				t.Fatalf("queue calls = %d, want one refresh after remove", service.queueCallCount())
			}
		})
	}
}

func TestRemoveAndDeleteCancellation(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha"})

	m.openRemoveDialog(false)
	m.update(tea.KeyPressMsg{Code: 'n'})
	if m.dialog != nil || len(service.recordedActions()) != 0 {
		t.Fatal("canceling remove started an action")
	}
	m.openRemoveDialog(false)
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil || len(service.recordedActions()) != 0 {
		t.Fatal("escaping remove started an action")
	}

	m.openRemoveDialog(true)
	m.dialog.fields[0].input.SetValue("")
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.status != "Type DELETE exactly to confirm" {
		t.Fatalf("empty delete status = %#v", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("delete")
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.status != "Type DELETE exactly to confirm" {
		t.Fatalf("lowercase delete status = %#v", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("DELETE")
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	actions := service.recordedActions()
	if len(actions) != 1 || actions[0].kind != "delete" || actions[0].hashes != "a" {
		t.Fatalf("delete actions = %#v", actions)
	}
}

func TestMoveAndControlActionsUseMarkedTargets(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service,
		client.TorrentInfo{Hash: "a", Name: "Alpha"},
		client.TorrentInfo{Hash: "b", Name: "Beta"},
	)
	m.queueIndex = 0
	m.toggleMark()
	m.queueIndex = 1
	m.toggleMark()
	m.openMoveDialog()
	m.dialog.fields[0].input.SetValue("/data/x")
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	for _, kind := range []string{"start", "pause", "force-start"} {
		runTUIWork(t, m, m.openAction(kind))
	}
	actions := service.recordedActions()
	if len(actions) != 4 {
		t.Fatalf("actions = %#v, want four actions", actions)
	}
	if actions[0].kind != "move" || actions[0].hashes != "a|b" || actions[0].value != "/data/x" {
		t.Fatalf("move action = %#v", actions[0])
	}
	for _, action := range actions[1:] {
		if action.hashes != "a|b" {
			t.Fatalf("control action = %#v", action)
		}
	}
}

func TestMoveBlankPathKeepsDialogOpen(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha"})
	m.openMoveDialog()
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.dialog == nil || m.dialog.status != "A destination path is required" {
		t.Fatalf("blank move status = %#v", m.dialog)
	}
	actions := service.recordedActions()
	if len(actions) != 0 {
		t.Fatalf("blank move actions = %#v", actions)
	}
}

func TestDestructiveAndEditActionsRejectEmptySelection(t *testing.T) {
	for _, action := range []string{"move", "remove", "edit"} {
		t.Run(action, func(t *testing.T) {
			m := newModel(t.Context(), &recordingService{})
			switch action {
			case "move":
				m.openMoveDialog()
			case "remove":
				m.openRemoveDialog(false)
			case "edit":
				m.openEditDialog()
			}
			if m.dialog != nil {
				t.Fatalf("%s opened dialog without a target: %#v", action, m.dialog)
			}
			if m.notice != "Select or mark a torrent first" {
				t.Fatalf("%s notice = %q", action, m.notice)
			}
		})
	}
}

func TestActionErrorPreservesMarksAndShowsFailure(t *testing.T) {
	service := &recordingService{actionErr: errors.New("server rejected action")}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha"})
	m.marked["a"] = true
	m.page = serverPage
	runTUIWork(t, m, m.launchAction("start", actionRequest{kind: "start", hashes: "a"}))
	if !m.failed || m.notice != "server rejected action" {
		t.Fatalf("failure notice = %q, failed=%t", m.notice, m.failed)
	}
	if !m.marked["a"] {
		t.Fatal("failed action cleared marks")
	}
}

func TestBusyConfirmDoesNotStartSecondAction(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{})
	service := &recordingService{block: block, started: started}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha"})
	first := m.openAction("start")
	if first == nil {
		t.Fatal("start action did not return a command")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- first() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("start action did not reach the backend")
	}
	m.openRemoveDialog(false)
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.status != "Finish the current request first" {
		t.Fatalf("busy status = %#v", m.dialog)
	}
	actions := service.recordedActions()
	if len(actions) != 1 || actions[0].kind != "start" {
		t.Fatalf("busy confirmation actions = %#v", actions)
	}
	close(block)
	runTUIWork(t, m, m.finishWork((<-done).(workDone)))
}

func TestDeleteMessageListsMarkedTorrentNames(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service,
		client.TorrentInfo{Hash: "a", Name: "Alpha"},
		client.TorrentInfo{Hash: "b", Name: "Beta"},
	)
	m.queueIndex = 0
	m.toggleMark()
	m.queueIndex = 1
	m.toggleMark()
	m.openRemoveDialog(true)
	if m.dialog == nil || !strings.Contains(m.dialog.message, "Delete 2 torrents?") ||
		!strings.Contains(m.dialog.message, "Alpha") || !strings.Contains(m.dialog.message, "Beta") {
		t.Fatalf("delete message = %q", m.dialog.message)
	}
}

func TestEditFocusedSeedtimePrefillsAndKeepsSingleNotice(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha", SeedingTimeLimit: 90})
	m.openEditDialog()
	for m.dialog.selected < 4 {
		m.update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.dialog.items[m.dialog.selected].action != "seedtime-limit" {
		t.Fatalf("selected field = %q", m.dialog.items[m.dialog.selected].action)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.value(0) != client.SeedtimeLimitInputValue(90) {
		t.Fatalf("seed time prefill = %#v", m.dialog)
	}
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	actions := service.recordedActions()
	if len(actions) != 1 || actions[0].kind != "set" || actions[0].hashes != "a" ||
		actions[0].field != "seedtime-limit" || actions[0].value != client.SeedtimeLimitInputValue(90) {
		t.Fatalf("focused edit action = %#v", actions)
	}
	if m.notice != "Saved torrent property" {
		t.Fatalf("single edit notice = %q", m.notice)
	}
}
