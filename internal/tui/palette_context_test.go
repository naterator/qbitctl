//go:build !plan9

package tui

import (
	"fmt"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func paletteItemsByAction(items []dialogItem) map[string]dialogItem {
	itemsByAction := make(map[string]dialogItem, len(items))
	for _, item := range items {
		if _, exists := itemsByAction[item.action]; exists {
			panic(fmt.Sprintf("duplicate palette action %q", item.action))
		}
		itemsByAction[item.action] = item
	}
	return itemsByAction
}

func requirePaletteActions(t *testing.T, items []dialogItem, actions ...string) map[string]dialogItem {
	t.Helper()
	byAction := paletteItemsByAction(items)
	for _, action := range actions {
		if _, ok := byAction[action]; !ok {
			t.Errorf("palette omitted action %q; actions = %v", action, paletteActionNames(items))
		}
	}
	return byAction
}

func requireNoPaletteActions(t *testing.T, items []dialogItem, actions ...string) {
	t.Helper()
	byAction := paletteItemsByAction(items)
	for _, action := range actions {
		if _, ok := byAction[action]; ok {
			t.Errorf("palette exposed unavailable action %q; actions = %v", action, paletteActionNames(items))
		}
	}
}

func paletteActionNames(items []dialogItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.action)
	}
	return names
}

func TestPaletteCommandsQueueContext(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{{Hash: "a", Name: "Alpha", State: "downloading"}})

	byAction := requirePaletteActions(t, m.paletteCommands(),
		"search", "add", "refresh", "filter", "sort-queue", "queue-columns", "toggle-mark",
		"details", "trackers", "edit", "start", "pause", "force-start", "move", "remove", "delete",
		"recheck", "reannounce", "queue-up", "queue-down", "queue-top", "queue-bottom", "rename",
		"previous-item", "next-item", "previous-page", "next-page", "first-item", "last-item",
	)
	if byAction["toggle-mark"].label != "Mark torrent" {
		t.Fatalf("unmarked toggle label = %q, want Mark torrent", byAction["toggle-mark"].label)
	}
	if _, ok := byAction["clear-marks"]; ok {
		t.Fatal("palette exposed clear marks without marks")
	}

	m.toggleMark()
	m.filter = "alpha"
	byAction = requirePaletteActions(t, m.paletteCommands(), "clear-marks", "clear-queue-filter")
	if byAction["toggle-mark"].label != "Unmark torrent" {
		t.Fatalf("marked toggle label = %q, want Unmark torrent", byAction["toggle-mark"].label)
	}

	// Local queue navigation and marking remain useful without a live backend,
	// while actions that would issue a qBittorrent mutation do not.
	m.backend = nil
	requirePaletteActions(t, m.paletteCommands(), "filter", "sort-queue", "toggle-mark", "clear-marks")
	requireNoPaletteActions(t, m.paletteCommands(), "add", "details", "trackers", "start", "pause", "move", "remove", "delete", "recheck", "reannounce", "queue-up", "queue-down", "queue-top", "queue-bottom", "rename")
}

func TestPaletteHidesRenameForMultipleMarkedTorrents(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{{Hash: "a", Name: "Alpha"}, {Hash: "b", Name: "Beta"}})
	m.marked["a"] = true
	m.marked["b"] = true
	byAction := requirePaletteActions(t, m.paletteCommands(), "recheck", "reannounce", "queue-up", "queue-down", "queue-top", "queue-bottom")
	if _, ok := byAction["rename"]; ok {
		t.Fatal("palette exposed rename for multiple marked torrents")
	}
}

func TestPaletteCommandsSearchContext(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.page = searchPage
	m.query = "ubuntu"
	m.allResults = []client.SearchResult{{ID: 123456789012, Name: "Ubuntu", Size: 4 << 30, Seeders: 10}}
	m.results = append([]client.SearchResult(nil), m.allResults...)
	m.resultFilter = resultFilter{query: "ubuntu"}

	byAction := requirePaletteActions(t, m.paletteCommands(),
		"search", "refresh", "providers", "filter-results", "sort-results", "clear-result-filter", "add-result",
		"previous-item", "next-item", "first-item", "last-item",
	)
	if byAction["search"].label != "New search" {
		t.Fatalf("new search label = %q, want New search", byAction["search"].label)
	}
	if byAction["refresh"].label != "Search again" || byAction["refresh"].keys != "r" {
		t.Fatalf("search refresh item = %#v, want Search again on r", byAction["refresh"])
	}
	if byAction["add-result"].label != "Add selected result" {
		t.Fatalf("selected result label = %q", byAction["add-result"].label)
	}
	if byAction["add-result"].keys != "enter" {
		t.Fatalf("selected result key hint = %q, want enter", byAction["add-result"].keys)
	}

	// Empty result sets still expose search filtering/sorting, and the add
	// command remains useful for entering a cached result ID by hand.
	m.query = ""
	m.allResults = nil
	m.results = nil
	m.resultFilter = resultFilter{}
	byAction = requirePaletteActions(t, m.paletteCommands(), "filter-results", "sort-results", "add-result")
	if byAction["add-result"].label != "Add result by ID" {
		t.Fatalf("manual result label = %q", byAction["add-result"].label)
	}
	if byAction["add-result"].keys != "" {
		t.Fatalf("manual result key hint = %q, want no Enter hint", byAction["add-result"].keys)
	}
	requireNoPaletteActions(t, m.paletteCommands(), "refresh", "previous-item", "next-item")

	// Search cancellation is global so opening the palette from another tab
	// cannot strand an active request.
	m.page = serverPage
	m.busy, m.work = "Searching", searchWork
	requirePaletteActions(t, m.paletteCommands(), "cancel-search")
	requireNoPaletteActions(t, m.paletteCommands(), "refresh", "filter-results", "sort-results", "add-result")
}

func TestPaletteRefreshesAfterSearchWork(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.page = searchPage
	m.query = "ubuntu"
	m.busy, m.work = "Searching", searchWork
	m.openPaletteDialog()
	m.dialog.fields[0].input.SetValue("search")
	m.dialog.refreshPalette()
	if _, ok := paletteItemsByAction(m.dialog.items)["cancel-search"]; !ok {
		t.Fatalf("filtered busy palette omitted cancel-search: %v", paletteActionNames(m.dialog.items))
	}

	m.Update(workDone{
		kind:                   searchWork,
		results:                []client.SearchResult{{ID: 123456789012, Name: "Ubuntu"}},
		searchPluginWasDefault: false,
	})
	if m.busy != "" {
		t.Fatalf("search completion left palette busy: %q", m.busy)
	}
	byAction := paletteItemsByAction(m.dialog.items)
	if _, ok := byAction["cancel-search"]; ok {
		t.Fatalf("stale cancel-search remained after completion: %v", paletteActionNames(m.dialog.items))
	}
	if item, ok := byAction["refresh"]; !ok || item.label != "Search again" {
		t.Fatalf("completed search palette omitted Search again: %v", paletteActionNames(m.dialog.items))
	}
}

func TestPaletteCommandsServerAndConfigContext(t *testing.T) {
	server := &recordingService{}
	m := newModel(t.Context(), server)
	m.page = serverPage
	m.serverRows = []detailRow{{label: "Connection", value: "connected"}}
	byAction := requirePaletteActions(t, m.paletteCommands(), "refresh", "server-download-limit", "server-upload-limit", "server-mode-normal", "server-mode-alternate")
	if byAction["refresh"].label != "Refresh server info" {
		t.Fatalf("server refresh label = %q", byAction["refresh"].label)
	}

	m.backend = nil
	m.page = configPage
	byAction = requirePaletteActions(t, m.paletteCommands(),
		"config-server", "config-username", "config-password", "config-default", "config-theme", "config-units", "config-refresh", "config-provider", "config-file", "config-cache",
		"previous-item", "next-item", "previous-page", "next-page", "first-item", "last-item",
	)
	if byAction["config-cache"].label != "Show search cache path" {
		t.Fatalf("search cache palette label = %q", byAction["config-cache"].label)
	}
	requireNoPaletteActions(t, m.paletteCommands(), "refresh", "add", "search")

	m.busy, m.work = "Connecting", connectWork
	requirePaletteActions(t, m.paletteCommands(), "cancel-connection")
	requireNoPaletteActions(t, m.paletteCommands(), "config-server", "config-units")
}
