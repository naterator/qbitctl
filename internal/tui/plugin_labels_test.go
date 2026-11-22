//go:build !plan9

package tui

import (
	"strings"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func TestSelectedSearchPluginAppearsInTUILabels(t *testing.T) {
	// The palette hides network actions until a backend is available. Keep this
	// label test deterministic with the same lightweight service used by the
	// other TUI unit tests instead of relying on a live qBittorrent connection.
	m := newModel(t.Context(), &recordingService{})
	m.searchPlugin = "prowlarr"
	m.searchPluginDefaultActive = false

	m.openSearchDialog()
	if m.dialog == nil || m.dialog.title != "Search Prowlarr" {
		t.Fatalf("search dialog title = %#v, want Search Prowlarr", m.dialog)
	}
	if got := m.dialog.fields[1].input.Placeholder; got != client.DefaultSearchPlugins {
		t.Fatalf("plugin placeholder = %q, want %s", got, client.DefaultSearchPlugins)
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
