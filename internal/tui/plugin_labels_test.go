//go:build !plan9

package tui

import (
	"strings"
	"testing"
)

func TestSelectedSearchPluginAppearsInTUILabels(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.searchPlugin = "prowlarr"

	m.openSearchDialog()
	if m.dialog == nil || m.dialog.title != "Search Prowlarr" {
		t.Fatalf("search dialog title = %#v, want Search Prowlarr", m.dialog)
	}
	if got := m.dialog.fields[1].input.Placeholder; got != "prowlarr" {
		t.Fatalf("plugin placeholder = %q, want prowlarr", got)
	}

	m.dialog = nil
	m.openPaletteDialog()
	if m.dialog == nil || !strings.Contains(strings.ToLower(m.dialog.items[0].description), "prowlarr") {
		t.Fatalf("palette search description = %#v, want selected plugin", m.dialog)
	}

	m.dialog = nil
	m.page = searchPage
	view := strings.Join(m.viewSearchList(80, 20), "\n")
	if !strings.Contains(view, "Press / to search Prowlarr.") {
		t.Fatalf("empty search view = %q, want selected plugin label", view)
	}
}
