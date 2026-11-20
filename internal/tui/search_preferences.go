//go:build !plan9

package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	searchPreferencesDirName  = "qbitctl"
	searchPreferencesFileName = "tui-search.json"
)

// searchPreferences is deliberately separate from resultFilter.  The free-form
// query describes the current search, while the numeric bounds and sort order
// are useful defaults for the next search and across TUI sessions.
type searchPreferences struct {
	Sort       resultSort `json:"sort,omitempty"`
	MinSeeders string     `json:"min_seeders,omitempty"`
	MinSize    string     `json:"min_size,omitempty"`
	MaxSize    string     `json:"max_size,omitempty"`
}

func searchPreferencesPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		if home == "" {
			return "", errors.New("home directory is empty")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, searchPreferencesDirName, searchPreferencesFileName), nil
}

func loadSearchPreferences(path string) (resultSort, resultFilter, error) {
	defaults := resultFilter{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return resultOriginal, defaults, nil
		}
		return resultOriginal, defaults, fmt.Errorf("read preferences: %w", err)
	}

	var disk searchPreferences
	if err := json.Unmarshal(data, &disk); err != nil {
		return resultOriginal, defaults, fmt.Errorf("decode preferences: %w", err)
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
	if path == "" {
		return errors.New("invalid preferences path")
	}
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
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tui-search-*.tmp")
	if err != nil {
		return fmt.Errorf("create preferences temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect preferences temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync preferences: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close preferences temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace preferences: %w", err)
	}
	return nil
}
