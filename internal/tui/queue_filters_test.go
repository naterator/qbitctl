//go:build !plan9

package tui

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func TestQueueCategoryAndTagFiltersShareLocalMatcher(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{
		{Name: "Linux ISO", Hash: "a", Category: "Linux", Tags: "iso,release", State: "downloading"},
		{Name: "Linux docs", Hash: "b", Category: "Linux", Tags: "docs", State: "seeding"},
		{Name: "Loose file", Hash: "c", Tags: "", State: "stoppedDL"},
	})

	m.setCategoryFilter("linux")
	if got := queueHashes(m.queue); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("category filter queue = %v, want a,b", got)
	}
	m.setTagFilter("release")
	if got := queueHashes(m.queue); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("combined category/tag queue = %v, want a", got)
	}
	m.setCategoryFilter(queueFilterUnassigned)
	if got := queueHashes(m.queue); !slices.Equal(got, []string{}) {
		t.Fatalf("unassigned category queue = %v, want empty while tag remains", got)
	}
	m.setTagFilter("")
	if got := queueHashes(m.queue); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("unassigned category queue = %v, want c", got)
	}
	m.setCategoryFilter("")
	if got := queueHashes(m.queue); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("cleared queue filters = %v, want a,b,c", got)
	}
	m.filter, m.stateFilter, m.categoryFilter, m.tagFilter = "linux", "active", "Linux", "release"
	m.rebuildQueue()
	m.openAction("clear-queue-filter")
	if m.filter != "" || m.stateFilter != "all" || m.categoryFilter != "" || m.tagFilter != "" {
		t.Fatalf("clear queue filter retained state: %#v", m.currentQueueFilter())
	}
}

func TestQueueFilterPickerLoadsServerCatalogAndAppliesSelection(t *testing.T) {
	backend := &queueFilterCatalogTestService{
		recordingService: recordingService{},
		categoriesValue:  []client.Category{{Name: "Zeta"}, {Name: "Alpha"}},
		tagsValue:        []string{"zeta", "alpha"},
	}
	m := newModel(t.Context(), backend)
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{{Name: "Alpha", Hash: "a", Category: "Alpha", Tags: "zeta"}})

	runTUIWork(t, m, m.openAction("filter-category"))
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatalf("category picker dialog = %#v, want static palette", m.dialog)
	}
	if got := m.dialog.items[2].label; got != "Alpha" {
		t.Fatalf("category picker first catalog item = %q, want Alpha", got)
	}
	m.openAction(m.dialog.items[2].action)
	if m.categoryFilter != "Alpha" || len(m.queue) != 1 {
		t.Fatalf("category selection = %q, queue=%d; want Alpha and one row", m.categoryFilter, len(m.queue))
	}

	runTUIWork(t, m, m.openAction("filter-tag"))
	if m.dialog == nil || m.dialog.items[2].label != "alpha" {
		t.Fatalf("tag picker = %#v, want alpha as first catalog item", m.dialog)
	}
	m.openAction(m.dialog.items[2].action)
	if m.tagFilter != "alpha" || len(m.queue) != 0 {
		t.Fatalf("tag selection = %q, queue=%d; want alpha and no rows", m.tagFilter, len(m.queue))
	}
}

func TestQueueFilterPickerFallsBackToAllTorrents(t *testing.T) {
	// recordingService intentionally does not implement queueFilterCatalogService,
	// so the picker must derive its choices from the complete queue snapshot.
	backend := &recordingService{}
	m := newModel(t.Context(), backend)
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{
		{Hash: "linux-a", Name: "Linux ISO", Category: "Linux", Tags: "alpha, beta", State: "downloading"},
		{Hash: "linux-b", Name: "Linux docs", Category: "Linux", Tags: "gamma", State: "seeding"},
		{Hash: "docs", Name: "Docs", Category: "Docs", Tags: "delta, alpha", State: "stoppedDL"},
		{Hash: "loose", Name: "Loose file", State: "stoppedDL"},
		{Hash: "loose-tagged", Name: "Tagged loose file", Tags: "beta, gamma", State: "stoppedDL"},
	})

	// Hide non-Linux rows first. The fallback must still discover the delta tag
	// and the loose-file categories from allTorrents, not only the visible queue.
	m.setCategoryFilter("Linux")
	m.openAction("filter-tag")
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatalf("fallback tag picker dialog = %#v, want static palette", m.dialog)
	}
	if got := pickerLabels(m.dialog); !slices.Equal(got, []string{"All tags", "Untagged", "alpha", "beta", "delta", "gamma"}) {
		t.Fatalf("fallback tag choices = %v", got)
	}
	m.openAction(queueFilterTagAction + "beta")
	if m.tagFilter != "beta" || !slices.Equal(queueHashes(m.queue), []string{"linux-a"}) {
		t.Fatalf("multi-tag selection = %q, queue=%v; want beta and linux-a", m.tagFilter, queueHashes(m.queue))
	}

	// Select the local Uncategorized sentinel, then the local Untagged
	// sentinel. Both choices must be represented by real queue filters rather
	// than being mistaken for an empty (cleared) selection.
	m.setTagFilter("")
	m.openAction("filter-category")
	m.openAction(pickerAction(m.dialog, "Uncategorized"))
	if m.categoryFilter != queueFilterUnassigned || !slices.Equal(queueHashes(m.queue), []string{"loose", "loose-tagged"}) {
		t.Fatalf("uncategorized selection = %q, queue=%v", m.categoryFilter, queueHashes(m.queue))
	}
	m.openAction("filter-tag")
	m.openAction(pickerAction(m.dialog, "Untagged"))
	if m.tagFilter != queueFilterUnassigned || !slices.Equal(queueHashes(m.queue), []string{"loose"}) {
		t.Fatalf("untagged selection = %q, queue=%v", m.tagFilter, queueHashes(m.queue))
	}
}

func TestResetRemoteStateClearsQueueCatalogSelections(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.categoryFilter = "Linux"
	m.tagFilter = "release"
	m.resetRemoteState()
	if m.categoryFilter != "" || m.tagFilter != "" {
		t.Fatalf("remote reset retained category/tag filters: %q / %q", m.categoryFilter, m.tagFilter)
	}
}

func queueHashes(torrents []client.TorrentInfo) []string {
	hashes := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		hashes = append(hashes, torrent.Hash)
	}
	sort.Strings(hashes)
	return hashes
}

func pickerLabels(d *dialog) []string {
	labels := make([]string, 0, len(d.items))
	for _, item := range d.items {
		labels = append(labels, item.label)
	}
	return labels
}

func pickerAction(d *dialog, label string) string {
	for _, item := range d.items {
		if item.label == label {
			return item.action
		}
	}
	return ""
}

type queueFilterCatalogTestService struct {
	recordingService
	categoriesValue []client.Category
	tagsValue       []string
}

func (s *queueFilterCatalogTestService) categories(context.Context) ([]client.Category, error) {
	return append([]client.Category(nil), s.categoriesValue...), nil
}

func (s *queueFilterCatalogTestService) tags(context.Context) ([]string, error) {
	return append([]string(nil), s.tagsValue...), nil
}
