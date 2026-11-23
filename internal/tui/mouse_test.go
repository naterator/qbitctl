//go:build !plan9

package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestMouseTabsAndVisibleRows(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.width, m.height = 80, 24
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{{Hash: "a", Name: "Alpha"}, {Hash: "b", Name: "Beta"}})
	if cmd := m.update(tea.MouseClickMsg{X: 17, Y: 1, Button: tea.MouseLeft}); cmd != nil {
		_ = cmd
	}
	if m.page != searchPage {
		t.Fatalf("tab click selected page %d, want search", m.page)
	}
	m.page = queuePage
	m.queueIndex = 0
	m.update(tea.MouseClickMsg{X: 10, Y: 6, Button: tea.MouseLeft})
	if m.queueIndex != 1 {
		t.Fatalf("queue row click selected %d, want second row", m.queueIndex)
	}
	m.update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.queueIndex != 0 {
		t.Fatalf("queue wheel moved to %d, want first row", m.queueIndex)
	}
}

func TestMouseSearchRowSelectionAndDialogSelection(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.width, m.height = 80, 24
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{{Name: "one"}, {Name: "two"}})
	m.update(tea.MouseClickMsg{X: 10, Y: 7, Button: tea.MouseLeft})
	if m.resultIndex != 1 {
		t.Fatalf("search row click selected %d, want second row", m.resultIndex)
	}
	m.openQueueColumnsDialog()
	panel := m.dialogPanelBounds()
	m.update(tea.MouseClickMsg{X: panel.left + 2, Y: panel.top + 2, Button: tea.MouseLeft})
	if m.dialog == nil || m.dialog.selected != 1 {
		t.Fatalf("dialog click selected %#v, want second item", m.dialog)
	}
	m.update(tea.MouseClickMsg{X: panel.left + 2, Y: panel.top + 2, Button: tea.MouseLeft})
	if m.dialog != nil || m.queueColumns != queueColumnsWide {
		t.Fatalf("second dialog click did not activate selection: dialog=%#v columns=%q", m.dialog, m.queueColumns)
	}
}

func TestMouseDialogWheelUsesVisibleSelectionAndRejectsOutsideClicks(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.width, m.height = 80, 24
	m.openQueueColumnsDialog()
	panel := m.dialogPanelBounds()
	if cmd := m.handleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown}); cmd != nil {
		t.Fatalf("dialog wheel returned command %v", cmd)
	}
	if m.dialog.selected != 1 {
		t.Fatalf("dialog wheel selected %d, want second option", m.dialog.selected)
	}
	selected := m.dialog.selected
	m.handleMouseClick(tea.MouseClickMsg{X: panel.left - 1, Y: panel.top + 2, Button: tea.MouseLeft})
	if m.dialog.selected != selected {
		t.Fatalf("outside dialog click changed selection to %d", m.dialog.selected)
	}
}

func TestMouseDetailsWheelMovesFileCursor(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.width, m.height = 80, 10
	m.dialog = &dialog{
		kind:           dialogDetails,
		torrentDetails: &torrentDetailTarget{hash: "hash"},
		detail:         newDetailDialogState(),
	}
	m.dialog.detail.tab = detailTabFiles
	m.dialog.detail.files = []client.TorrentFile{
		{Index: 0, Name: "one.bin"},
		{Index: 1, Name: "two.bin"},
	}
	m.handleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := m.dialog.detail.fileCursor; got != 1 {
		t.Fatalf("details wheel file cursor = %d, want 1", got)
	}
}

func TestMouseProfilePickerSelectsVisibleRow(t *testing.T) {
	m := newModel(context.Background(), nil)
	m.width, m.height = 80, 24
	m.dialog = &dialog{
		kind:     dialogProfilePicker,
		title:    "Profiles",
		items:    []dialogItem{{label: "default", action: "default"}, {label: "home", action: "home"}},
		selected: 0,
	}
	panel := m.dialogPanelBounds()
	headerRows := 1
	if panel.height >= 11 {
		headerRows = 3
	}
	m.handleDialogMouseClick(panel.left+2, panel.top+headerRows+1)
	if m.dialog.selected != 1 {
		t.Fatalf("profile picker click selected %d, want second row", m.dialog.selected)
	}
}
