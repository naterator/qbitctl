//go:build !plan9

package tui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type resultSort string

const (
	resultOriginal    resultSort = "original"
	resultSeedersDesc resultSort = "seeders-desc"
	resultSeedersAsc  resultSort = "seeders-asc"
	resultSizeDesc    resultSort = "size-desc"
	resultSizeAsc     resultSort = "size-asc"
)

var resultSortChoices = []struct {
	order resultSort
	label string
}{
	{resultOriginal, "Search order"},
	{resultSeedersDesc, "Most seeders"},
	{resultSeedersAsc, "Fewest seeders"},
	{resultSizeDesc, "Largest size"},
	{resultSizeAsc, "Smallest size"},
}

func (order resultSort) label() string {
	for _, choice := range resultSortChoices {
		if order == choice.order {
			return choice.label
		}
	}
	return resultSortChoices[0].label
}

func (order resultSort) compare(left, right client.SearchResult) int {
	switch order {
	case resultSeedersDesc, resultSeedersAsc:
		return compareResultMetric(int64(left.Seeders), int64(right.Seeders), order == resultSeedersDesc)
	case resultSizeDesc, resultSizeAsc:
		return compareResultMetric(left.Size, right.Size, order == resultSizeDesc)
	default:
		return 0
	}
}

func compareResultMetric(left, right int64, descending bool) int {
	if left < 0 {
		if right < 0 {
			return 0
		}
		return 1
	}
	if right < 0 {
		return -1
	}
	if descending {
		return cmp.Compare(right, left)
	}
	return cmp.Compare(left, right)
}

func (m *model) replaceSearchResults(results []client.SearchResult) {
	m.allResults = results
	m.results, m.resultIndex = nil, 0
	m.rebuildSearchResults()
}

func (m *model) setResultSort(order resultSort) {
	if order == m.resultSort {
		return
	}
	m.resultSort = order
	m.rebuildSearchResults()
	m.persistSearchPreferences(order, nil)
}

type resultBound struct {
	text  string
	value int64
}

type resultFilter struct {
	query                        string
	minSeeders, minSize, maxSize resultBound
}

func (f resultFilter) active() bool { return f != (resultFilter{}) }

func (f resultFilter) numeric() resultFilter {
	f.query = ""
	return f
}

func (m *model) setResultFilter(filter resultFilter) {
	m.resultFilter = filter
	m.rebuildSearchResults()
}

func (m *model) clearResultFilter() {
	hadBounds := m.resultFilter.numeric().active()
	m.setResultFilter(resultFilter{})
	if hadBounds {
		m.persistSearchPreferences("", &m.resultFilter)
	}
}

func (m *model) persistSearchPreferences(order resultSort, filter *resultFilter) {
	if m.preferencesPath == "" {
		return
	}
	if err := saveSearchPreferences(m.preferencesPath, order, filter); err != nil {
		m.notice, m.failed = "Could not save search preferences. "+err.Error(), true
		return
	}
	m.notice, m.failed = "Search preferences saved", false
}

func (m *model) rebuildSearchResults() {
	previous, selected := m.focusedResult()
	terms := strings.Fields(strings.ToLower(m.resultFilter.query))
	m.results = m.results[:0]
	for _, result := range m.allResults {
		if m.resultFilter.matches(result, terms) {
			m.results = append(m.results, result)
		}
	}
	if m.resultSort != resultOriginal {
		slices.SortStableFunc(m.results, m.resultSort.compare)
	}
	m.resultIndex = max(0, min(m.resultIndex, len(m.results)-1))
	if selected {
		if index := slices.Index(m.results, previous); index >= 0 {
			m.resultIndex = index
		}
	}
}

func (f resultFilter) matches(result client.SearchResult, terms []string) bool {
	if f.minSeeders.text != "" && int64(result.Seeders) < f.minSeeders.value {
		return false
	}
	if (f.minSize.text != "" || f.maxSize.text != "") && result.Size < 0 {
		return false
	}
	if f.minSize.text != "" && result.Size < f.minSize.value {
		return false
	}
	if f.maxSize.text != "" && result.Size > f.maxSize.value {
		return false
	}
	if len(terms) == 0 {
		return true
	}
	text := strings.ToLower(result.Name + " " + result.Source)
	if result.ID > 0 {
		text += " " + strconv.FormatInt(result.ID, 10)
	}
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

var resultFilterFields = [...]struct {
	label       string
	placeholder string
}{
	{"Text", "title, source, or ID"},
	{"Min seeders", "optional, e.g. 10"},
	{"Min size", "optional, e.g. 500 MB"},
	{"Max size", "optional, e.g. 4 GiB"},
}

type resultFilterEditor struct {
	inputs        [len(resultFilterFields)]textinput.Model
	active        int
	problem       string
	previous      resultFilter
	previousIndex int
}

func (m *model) startResultFilter() tea.Cmd {
	m.dialog = nil
	if len(m.allResults) == 0 && !m.resultFilter.numeric().active() {
		m.notice, m.failed = "Run a search to filter its results", false
		return nil
	}
	filter := m.resultFilter
	editor := &resultFilterEditor{previous: filter, previousIndex: m.resultIndex}
	values := [...]string{filter.query, filter.minSeeders.text, filter.minSize.text, filter.maxSize.text}
	for i, field := range resultFilterFields {
		input := textinput.New()
		input.Prompt = ""
		input.Placeholder = field.placeholder
		input.SetStyles(viewInputStyle(viewFieldColor))
		input.SetValue(values[i])
		editor.inputs[i] = input
	}
	m.resultEditor = editor
	return m.focusResultFilterField(0)
}

func (m *model) focusResultFilterField(index int) tea.Cmd {
	editor := m.resultEditor
	editor.inputs[editor.active].Blur()
	editor.active = (index + len(editor.inputs)) % len(editor.inputs)
	active := &editor.inputs[editor.active]
	active.SetWidth(m.resultFilterInputWidth())
	return active.Focus()
}

func (m *model) updateResultFilter(msg tea.Msg) tea.Cmd {
	editor := m.resultEditor
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			if m.previewResultFilter() {
				changedBounds := m.resultFilter.numeric() != editor.previous.numeric()
				m.resultEditor = nil
				if changedBounds {
					m.persistSearchPreferences("", &m.resultFilter)
				}
			}
			return nil
		case "esc":
			m.setResultFilter(editor.previous)
			m.resultIndex = max(0, min(editor.previousIndex, len(m.results)-1))
			m.resultEditor = nil
			return nil
		case "tab":
			return m.focusResultFilterField(editor.active + 1)
		case "shift+tab":
			return m.focusResultFilterField(editor.active - 1)
		case "up":
			m.moveCursor(-1)
			return nil
		case "down":
			m.moveCursor(1)
			return nil
		}
	}
	active := &editor.inputs[editor.active]
	previous := active.Value()
	var cmd tea.Cmd
	*active, cmd = active.Update(msg)
	if active.Value() != previous {
		m.previewResultFilter()
	}
	return cmd
}

func (m *model) previewResultFilter() bool {
	editor := m.resultEditor
	var values [len(resultFilterFields)]string
	for i := range editor.inputs {
		values[i] = strings.TrimSpace(editor.inputs[i].Value())
	}
	filter, err := parseResultFilter(values)
	if err != nil {
		editor.problem = err.Error()
		return false
	}
	editor.problem = ""
	if filter != m.resultFilter {
		m.setResultFilter(filter)
	}
	return true
}

func parseResultFilter(values [len(resultFilterFields)]string) (resultFilter, error) {
	filter := resultFilter{query: values[0]}
	if values[1] != "" {
		seeders, err := strconv.ParseInt(values[1], 10, 64)
		if err != nil || seeders < 0 {
			return resultFilter{}, errors.New("Minimum seeders must be a nonnegative whole number")
		}
		filter.minSeeders = resultBound{text: values[1], value: seeders}
	}
	for i, bound := range []*resultBound{&filter.minSize, &filter.maxSize} {
		value := values[i+2]
		if value == "" {
			continue
		}
		size, err := parseResultSize(value)
		if err != nil {
			return resultFilter{}, fmt.Errorf("%s is invalid (%w)", resultFilterFields[i+2].label, err)
		}
		*bound = resultBound{text: value, value: size}
	}
	if filter.minSize.text != "" && filter.maxSize.text != "" && filter.minSize.value > filter.maxSize.value {
		return resultFilter{}, errors.New("Minimum size cannot exceed maximum size")
	}
	return filter, nil
}
