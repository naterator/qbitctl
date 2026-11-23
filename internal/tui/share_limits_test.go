//go:build !plan9

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func selectEditField(t *testing.T, m *model, field string) {
	t.Helper()
	if m.dialog == nil || m.dialog.kind != dialogEditSelect {
		t.Fatalf("edit dialog = %#v, want field picker", m.dialog)
	}
	for i, item := range m.dialog.items {
		if item.action == field {
			m.dialog.selected = i
			return
		}
	}
	t.Fatalf("edit field %q not present in %#v", field, m.dialog.items)
}

func selectShareChoice(t *testing.T, m *model, choice string) {
	t.Helper()
	if m.dialog == nil || m.dialog.kind != dialogShareChoice {
		t.Fatalf("share choice dialog = %#v, want share picker", m.dialog)
	}
	for i, item := range m.dialog.items {
		if item.action == choice {
			m.dialog.selected = i
			return
		}
	}
	t.Fatalf("share choice %q not present in %#v", choice, m.dialog.items)
}

func TestShareLimitModeChoiceSubmitsSetRequest(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha", ShareLimitsMode: "Default"})
	m.openEditDialog()
	selectEditField(t, m, "share-limits-mode")
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	selectShareChoice(t, m, "MatchAny")
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))

	actions := service.recordedActions()
	if len(actions) != 1 || actions[0].kind != "set" || actions[0].field != "share-limits-mode" || actions[0].value != "MatchAny" || actions[0].hashes != "a" {
		t.Fatalf("share mode action = %#v", actions)
	}
}

func TestDestructiveShareLimitActionRequiresConfirmation(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "a", Name: "Alpha", ShareLimitAction: "Default"})

	openActionChoice := func() {
		m.openEditDialog()
		selectEditField(t, m, "share-limit-action")
		m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
		selectShareChoice(t, m, "RemoveWithContent")
		m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}

	openActionChoice()
	if m.dialog == nil || m.dialog.kind != dialogConfirm || m.dialog.confirmAction != "edit" || m.dialog.request.kind != "set" {
		t.Fatalf("destructive share confirmation = %#v", m.dialog)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(service.recordedActions()) != 0 {
		t.Fatal("canceling destructive share action submitted a request")
	}

	openActionChoice()
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	actions := service.recordedActions()
	if len(actions) != 1 || actions[0].kind != "set" || actions[0].field != "share-limit-action" || actions[0].value != "RemoveWithContent" || actions[0].hashes != "a" {
		t.Fatalf("confirmed share action = %#v", actions)
	}
}
