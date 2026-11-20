//go:build !plan9

package tui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type resultPreferenceTestService struct {
	service
	results []client.SearchResult
}

func (s resultPreferenceTestService) search(context.Context, string, string, time.Duration) ([]client.SearchResult, string, error) {
	return s.results, "", nil
}

func TestNewSearchCarriesSortAndBounds(t *testing.T) {
	const gib = 1 << 30
	backend := resultPreferenceTestService{results: []client.SearchResult{
		{ID: 1, Name: "Previous title", Seeders: 12, Size: gib},
		{ID: 2, Name: "New title", Seeders: 15, Size: 3 * gib},
		{ID: 3, Name: "Too few seeders", Seeders: 2, Size: 2 * gib},
		{ID: 4, Name: "Another release", Seeders: 13, Size: 2 * gib},
	}}
	m := newModel(t.Context(), backend)
	m.query = "previous search"
	m.setResultSort(resultSizeAsc)
	filter, err := parseResultFilter([4]string{"Previous title", "10", "2 GiB", "4 GiB"})
	if err != nil {
		t.Fatal(err)
	}
	m.setResultFilter(filter)
	finish := m.launchSearch("new search", "jackett", time.Second)
	m.finishWork(finish().(workDone))
	m.workers.Wait()
	if m.resultFilter != filter.numeric() || m.resultSort != resultSizeAsc {
		t.Fatal("a new search did not keep the same numeric bounds and sort")
	}
	if len(m.results) != 2 || m.results[0].ID != 4 || m.results[1].ID != 2 {
		t.Fatalf("new results did not apply the saved bounds and sort: %v", m.results)
	}
}

func TestSearchPreferencesSaveOnlyCommittedBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qbitctl", "tui-search.json")
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.loadSearchPreferences(path)
	m.setResultSort(resultSeedersDesc)
	m.replaceSearchResults([]client.SearchResult{{ID: 1, Name: "Ubuntu", Seeders: 12, Size: 3 << 30}})
	m.handleKey("f")
	for i, value := range []string{"Ubuntu", "10", "2 GiB", "4 GiB"} {
		if i > 0 {
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		}
		m.Update(tea.PasteMsg{Content: value})
	}
	reopened := newModel(t.Context(), nil)
	reopened.loadSearchPreferences(path)
	if reopened.resultSort != resultSeedersDesc || reopened.resultFilter.active() {
		t.Fatal("uncommitted numeric filters were saved")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	reopened.loadSearchPreferences(path)
	if reopened.resultSort != resultSeedersDesc || reopened.resultFilter != m.resultFilter.numeric() {
		t.Fatal("committed bounds and sort were not restored independently of the text filter")
	}
	m.handleKey("esc")
	reopened.loadSearchPreferences(path)
	if reopened.resultSort != resultSeedersDesc || reopened.resultFilter.active() {
		t.Fatal("clearing filters did not clear the saved bounds and keep the sort")
	}
}

func TestResultSortPickerShowsAvailableOrders(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.setResultSort(resultSizeAsc)
	m.handleKey("s")
	view := m.dialogView(80, 24)
	for _, choice := range resultSortChoices {
		if !strings.Contains(view, choice.label) {
			t.Errorf("sort picker omitted %q", choice.label)
		}
	}
	if !strings.Contains(view, "› Smallest size") {
		t.Fatal("sort picker did not highlight the current order")
	}
}

func TestResultOrderKeepsFocusAndSearchOrder(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{
		{ID: 11, Name: "Ubuntu Desktop", Seeders: 5, Size: 6 << 30},
		{ID: 12, Name: "Debian", Seeders: 27, Size: 4 << 30},
		{ID: 13, Name: "Ubuntu Server", Seeders: 27, Size: 2 << 30},
		{ID: 14, Name: "Unknown", Seeders: -1, Size: -1},
		{ID: 15, Name: "Fedora", Seeders: 0, Size: 5 << 30},
	})
	m.resultIndex = 2
	for _, tt := range []struct {
		order resultSort
		ids   []int64
	}{
		{resultSeedersDesc, []int64{12, 13, 11, 15, 14}},
		{resultSeedersAsc, []int64{15, 11, 12, 13, 14}},
		{resultSizeDesc, []int64{11, 15, 12, 13, 14}},
		{resultSizeAsc, []int64{13, 12, 15, 11, 14}},
		{resultOriginal, []int64{11, 12, 13, 14, 15}},
	} {
		m.setResultSort(tt.order)
		got := make([]int64, 0, len(m.results))
		for _, result := range m.results {
			got = append(got, result.ID)
		}
		if !slices.Equal(got, tt.ids) {
			t.Errorf("%s order = %v, want %v", tt.order, got, tt.ids)
		}
		if focused, ok := m.focusedResult(); !ok || focused.ID != 13 {
			t.Errorf("%s changed focused result to %v", tt.order, focused)
		}
	}
}

func TestResultFilterEditsLoadedResults(t *testing.T) {
	// No backend is needed when narrowing a result set.
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{
		{ID: 123456789001, Name: "Ubuntu Desktop", Source: "example.net"},
		{ID: 123456789002, Name: "Ubuntu Server", Source: "example.org"},
		{ID: 123456789003, Name: "Debian", Source: "example.org"},
	})
	m.handleKey("f")
	if m.resultEditor == nil {
		t.Fatal("f did not open the inline result filter")
	}
	m.Update(tea.PasteMsg{Content: "UBUNTU example.org 9002"})
	if focused, ok := m.focusedResult(); len(m.results) != 1 || !ok || focused.ID != 123456789002 {
		t.Fatalf("live filtering selected %v from %v", focused, m.results)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resultEditor != nil || len(m.results) != 1 || len(m.allResults) != 3 {
		t.Fatal("committing the filter changed the loaded result set")
	}
	m.handleKey("esc")
	if m.resultFilter.active() || len(m.results) != 3 {
		t.Fatal("Escape did not restore all loaded results")
	}
	m.handleKey("f")
	m.Update(tea.PasteMsg{Content: "none"})
	if len(m.results) != 0 {
		t.Fatal("results remained visible under a non-matching filter")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if focused, ok := m.focusedResult(); !ok || focused.ID != 123456789002 {
		t.Fatalf("canceling filter editing did not restore focus: %v", focused)
	}
}

func TestResultRangeFilterCombinesCriteriaAndKeepsSort(t *testing.T) {
	const gib = 1 << 30
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{
		{ID: 1, Name: "Ubuntu minimum", Seeders: 10, Size: 2 * gib},
		{ID: 2, Name: "Ubuntu maximum", Seeders: 20, Size: 4 * gib},
		{ID: 3, Name: "Ubuntu few seeds", Seeders: 9, Size: 3 * gib},
		{ID: 4, Name: "Ubuntu small", Seeders: 12, Size: gib},
		{ID: 5, Name: "Ubuntu large", Seeders: 30, Size: 5 * gib},
		{ID: 6, Name: "Debian", Seeders: 30, Size: 3 * gib},
		{ID: 7, Name: "Ubuntu unknown seeds", Seeders: -1, Size: 3 * gib},
		{ID: 8, Name: "Ubuntu unknown size", Seeders: 30, Size: -1},
	})
	m.setResultSort(resultSeedersDesc)
	m.handleKey("f")
	for i, value := range []string{"Ubuntu", "10", "2 GiB", "4 GiB"} {
		if i > 0 {
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		}
		m.Update(tea.PasteMsg{Content: value})
	}
	ids := make([]int64, 0, len(m.results))
	for _, result := range m.results {
		ids = append(ids, result.ID)
	}
	if !slices.Equal(ids, []int64{2, 1}) {
		t.Fatalf("combined results = %v, want both inclusive boundaries in seed order", ids)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resultEditor != nil || len(m.allResults) != 8 || m.resultSort != resultSeedersDesc {
		t.Fatal("keeping the filter changed the loaded results or sort")
	}

	for _, tt := range []struct {
		values   [4]string
		matching []int64
	}{
		{[4]string{"", "", "", "2 GiB"}, []int64{1, 4}},
		{[4]string{"", "", "4 GiB", ""}, []int64{2, 5}},
	} {
		filter, err := parseResultFilter(tt.values)
		if err != nil {
			t.Fatal(err)
		}
		var matching []int64
		for _, result := range m.allResults {
			if filter.matches(result, nil) {
				matching = append(matching, result.ID)
			}
		}
		if !slices.Equal(matching, tt.matching) {
			t.Errorf("open-ended bounds %v selected %v, want %v", tt.values, matching, tt.matching)
		}
	}
}

func TestResultRangeFilterRejectsInvalidDraft(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{{ID: 1, Name: "Ubuntu", Size: 3 << 30}})
	m.handleKey("f")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.PasteMsg{Content: "-1"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resultEditor == nil || !strings.Contains(m.resultEditor.problem, "whole number") || m.resultFilter.active() {
		t.Fatal("invalid seed count was applied")
	}
	m.resultEditor.inputs[1].SetValue("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.PasteMsg{Content: "2 GiB"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.PasteMsg{Content: "1 GiB"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resultEditor == nil || !strings.Contains(m.resultEditor.problem, "exceed") || len(m.results) != 1 {
		t.Fatal("invalid size range was applied")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.resultEditor != nil || m.resultFilter.active() {
		t.Fatal("Escape did not restore the original filters")
	}
}
