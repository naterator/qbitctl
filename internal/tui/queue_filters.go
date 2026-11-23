//go:build !plan9

package tui

import (
	"context"
	"sort"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// These sentinels distinguish the picker choices for unassigned rows from the
// empty value used to mean "no category/tag filter". They never go on the
// wire; they are local to the queue view.
const (
	queueFilterCategoryAction = "queue-filter-category:"
	queueFilterTagAction      = "queue-filter-tag:"
	queueFilterUnassigned     = "\x00unassigned"
)

type queueFilter struct {
	query    string
	state    string
	category string
	tag      string
}

func (m *model) currentQueueFilter() queueFilter {
	return queueFilter{
		query:    m.filter,
		state:    m.stateFilter,
		category: m.categoryFilter,
		tag:      m.tagFilter,
	}
}

// setQueueFilter is the single mutation path for all queue filter dimensions.
// Keeping text/state/category/tag together ensures every picker and the
// existing text dialog rebuild the same local queue view and clear marks only
// when the effective filter actually changes.
func (m *model) setQueueFilter(next queueFilter) {
	next.query = strings.TrimSpace(next.query)
	next.state = strings.TrimSpace(next.state)
	if next.state == "" {
		next.state = "all"
	}
	next.category = strings.TrimSpace(next.category)
	next.tag = strings.TrimSpace(next.tag)
	previous := m.currentQueueFilter()
	if previous != next {
		m.clearMarks()
	}
	m.filter = next.query
	m.stateFilter = next.state
	m.categoryFilter = next.category
	m.tagFilter = next.tag
	m.rebuildQueue()
}

func (m *model) setCategoryFilter(value string) {
	next := m.currentQueueFilter()
	next.category = value
	m.setQueueFilter(next)
}

func (m *model) setTagFilter(value string) {
	next := m.currentQueueFilter()
	next.tag = value
	m.setQueueFilter(next)
}

func queueFilterMatches(torrent client.TorrentInfo, filter queueFilter) bool {
	if !matchesState(torrent, filter.state) {
		return false
	}
	if filter.category != "" {
		if filter.category == queueFilterUnassigned {
			if strings.TrimSpace(torrent.Category) != "" {
				return false
			}
		} else if !strings.EqualFold(strings.TrimSpace(torrent.Category), filter.category) {
			return false
		}
	}
	if filter.tag != "" {
		if filter.tag == queueFilterUnassigned {
			if torrentHasAnyTag(torrent.Tags) {
				return false
			}
		} else if !torrentHasTag(torrent.Tags, filter.tag) {
			return false
		}
	}
	query := strings.Fields(strings.ToLower(filter.query))
	if len(query) == 0 {
		return true
	}
	index := strings.ToLower(strings.Join([]string{torrent.Name, torrent.Hash, torrent.Category, torrent.Tags}, " "))
	for _, term := range query {
		if !strings.Contains(index, term) {
			return false
		}
	}
	return true
}

func torrentHasTag(tags, wanted string) bool {
	for _, tag := range strings.Split(tags, ",") {
		if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func torrentHasAnyTag(tags string) bool {
	for _, tag := range strings.Split(tags, ",") {
		if strings.TrimSpace(tag) != "" {
			return true
		}
	}
	return false
}

func (m *model) openQueueCatalogPicker(kind string, categories []client.Category, tags []string) {
	items := make([]dialogItem, 0, 2+len(categories)+len(tags))
	switch kind {
	case "category":
		items = append(items,
			dialogItem{group: "Categories", label: "All categories", action: queueFilterCategoryAction},
			dialogItem{group: "Categories", label: "Uncategorized", action: queueFilterCategoryAction + queueFilterUnassigned},
		)
		seen := make(map[string]struct{}, len(categories))
		names := make([]string, 0, len(categories))
		for _, category := range categories {
			name := strings.TrimSpace(category.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			left, right := strings.ToLower(names[i]), strings.ToLower(names[j])
			if left == right {
				return names[i] < names[j]
			}
			return left < right
		})
		for _, name := range names {
			items = append(items, dialogItem{group: "Categories", label: viewSafe(name), action: queueFilterCategoryAction + name})
		}
		m.dialog = newQueueFilterPickerDialog("Filter by category", items)
	case "tag":
		items = append(items,
			dialogItem{group: "Tags", label: "All tags", action: queueFilterTagAction},
			dialogItem{group: "Tags", label: "Untagged", action: queueFilterTagAction + queueFilterUnassigned},
		)
		seen := make(map[string]struct{}, len(tags))
		names := make([]string, 0, len(tags))
		for _, tag := range tags {
			name := strings.TrimSpace(tag)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			left, right := strings.ToLower(names[i]), strings.ToLower(names[j])
			if left == right {
				return names[i] < names[j]
			}
			return left < right
		})
		for _, name := range names {
			items = append(items, dialogItem{group: "Tags", label: viewSafe(name), action: queueFilterTagAction + name})
		}
		m.dialog = newQueueFilterPickerDialog("Filter by tag", items)
	default:
		return
	}
	if m.dialog != nil {
		m.dialog.focusField(0)
	}
}

func newQueueFilterPickerDialog(title string, items []dialogItem) *dialog {
	// The action prefix is what makes the static palette a picker without
	// duplicating dialog navigation or selection rendering.
	return &dialog{
		kind:          dialogPalette,
		title:         title,
		fields:        []dialogField{newDialogField("", "", "Type to filter choices", "Enter applies the local queue filter")},
		allItems:      append([]dialogItem(nil), items...),
		items:         append([]dialogItem(nil), items...),
		paletteStatic: true,
	}
}

func (m *model) openQueueCatalogAction(kind string) tea.Cmd {
	if m.page != queuePage || m.backend == nil || m.busy != "" {
		return nil
	}
	service, ok := m.backend.(queueFilterCatalogService)
	if !ok {
		if kind == "category" {
			m.openQueueCatalogPicker(kind, queueCategoriesFromTorrents(m.allTorrents), nil)
		} else {
			m.openQueueCatalogPicker(kind, nil, queueTagsFromTorrents(m.allTorrents))
		}
		return nil
	}
	label := "Loading categories"
	if kind == "tag" {
		label = "Loading tags"
	}
	return m.startWork(queueCatalogWork, label, func(ctx context.Context) workDone {
		if kind == "category" {
			categories, err := service.categories(ctx)
			return workDone{catalogFilterKind: kind, categories: categories, err: err}
		}
		tags, err := service.tags(ctx)
		return workDone{catalogFilterKind: kind, tags: tags, err: err}
	})
}

func queueCategoriesFromTorrents(torrents []client.TorrentInfo) []client.Category {
	seen := make(map[string]struct{})
	result := make([]client.Category, 0)
	for _, torrent := range torrents {
		name := strings.TrimSpace(torrent.Category)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, client.Category{Name: name})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
		if left == right {
			return result[i].Name < result[j].Name
		}
		return left < right
	})
	return result
}

func queueTagsFromTorrents(torrents []client.TorrentInfo) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, torrent := range torrents {
		for _, raw := range strings.Split(torrent.Tags, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, name)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i]), strings.ToLower(result[j])
		if left == right {
			return result[i] < result[j]
		}
		return left < right
	})
	return result
}
