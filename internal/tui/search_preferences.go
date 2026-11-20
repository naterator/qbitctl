//go:build !plan9

package tui

import "fmt"

const searchPreferencesFileName = "tui-search.json"

// searchPreferences is deliberately separate from resultFilter.  The free-form
// query describes the current search, while the numeric bounds and sort order
// are useful defaults for the next search and across TUI sessions.
type searchPreferences struct {
	Sort       resultSort `json:"sort,omitempty"`
	MinSeeders string     `json:"min_seeders,omitempty"`
	MinSize    string     `json:"min_size,omitempty"`
	MaxSize    string     `json:"max_size,omitempty"`
}

func loadSearchPreferences(path string) (resultSort, resultFilter, error) {
	defaults := resultFilter{}
	var disk searchPreferences
	if err := readPreferences(path, &disk); err != nil {
		return resultOriginal, defaults, err
	}

	order := disk.Sort
	if order == "" {
		order = resultOriginal
	}
	if !knownResultSort(order) {
		return resultOriginal, defaults, fmt.Errorf("invalid result sort %q", order)
	}
	values := [...]string{"", disk.MinSeeders, disk.MinSize, disk.MaxSize}
	filter, err := parseResultFilter(values)
	if err != nil {
		return resultOriginal, defaults, fmt.Errorf("invalid search bounds: %w", err)
	}
	return order, filter, nil
}

func knownResultSort(order resultSort) bool {
	for _, choice := range resultSortChoices {
		if choice.order == order {
			return true
		}
	}
	return false
}

func saveSearchPreferences(path string, order resultSort, filter resultFilter) error {
	if !knownResultSort(order) {
		return fmt.Errorf("invalid result sort %q", order)
	}
	values := [...]string{"", filter.minSeeders.text, filter.minSize.text, filter.maxSize.text}
	validated, err := parseResultFilter(values)
	if err != nil {
		return fmt.Errorf("invalid search bounds: %w", err)
	}
	disk := searchPreferences{
		Sort:       order,
		MinSeeders: validated.minSeeders.text,
		MinSize:    validated.minSize.text,
		MaxSize:    validated.maxSize.text,
	}
	return writePreferences(path, disk)
}
