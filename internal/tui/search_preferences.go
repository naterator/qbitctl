//go:build !plan9

package tui

import (
	"encoding/json"
	"fmt"
)

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
	data, err := readPreferences(path, "search")
	if err != nil {
		return resultOriginal, resultFilter{}, err
	}
	return decodeSearchPreferences(data)
}

func decodeSearchPreferences(data json.RawMessage) (resultSort, resultFilter, error) {
	defaults := resultFilter{}
	var disk searchPreferences
	if len(data) > 0 {
		if err := json.Unmarshal(data, &disk); err != nil {
			return resultOriginal, defaults, err
		}
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

func saveSearchPreferences(path string, order resultSort, filter *resultFilter) error {
	fields := make(map[string]string)
	if order != "" {
		if !knownResultSort(order) {
			return fmt.Errorf("invalid result sort %q", order)
		}
		fields["sort"] = string(order)
	}
	if filter != nil {
		values := [...]string{"", filter.minSeeders.text, filter.minSize.text, filter.maxSize.text}
		validated, err := parseResultFilter(values)
		if err != nil {
			return fmt.Errorf("invalid search bounds: %w", err)
		}
		fields["min_seeders"] = validated.minSeeders.text
		fields["min_size"] = validated.minSize.text
		fields["max_size"] = validated.maxSize.text
	}
	return writePreferences(path, "search", fields)
}
