package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
	"github.com/naterator/qbitctl/internal/searchresults"
)

func displaySettings() []savedSetting {
	return []savedSetting{
		choicePreference("queue-sort", "Choose the TUI queue sort order", "queue", "sort", configfile.QueueSortValues()),
		choicePreference("queue-columns", "Choose the TUI queue column preset", "queue", "columns", []string{configfile.QueueColumnsCompact, configfile.QueueColumnsWide}),
		choicePreference("search-sort", "Choose the TUI search result sort order", "search", "sort", searchresults.SortValues()),
		searchBoundSetting("search-min-seeders", "Set the TUI search minimum seeder count", "min_seeders", "COUNT|none"),
		searchBoundSetting("search-min-size", "Set the TUI search minimum result size", "min_size", "SIZE|none"),
		searchBoundSetting("search-max-size", "Set the TUI search maximum result size", "max_size", "SIZE|none"),
	}
}

func choicePreference(name, description, section, key string, values []string) savedSetting {
	validate := func(value string) error {
		if !slices.Contains(values, value) {
			return fmt.Errorf("invalid %s %q (see qbitctl config %s list)", name, value, name)
		}
		return nil
	}
	choices := make([]settingChoice, 0, len(values))
	for _, value := range values {
		choices = append(choices, settingChoice{value: value})
	}
	return savedSetting{
		name: name, short: description, arg: "VALUE",
		get: func(path string) (string, error) {
			value, err := readDisplayPreference(path, section, key, values[0])
			if err != nil {
				return "", err
			}
			return value, validate(value)
		},
		set: func(path, value string) error {
			value = strings.ToLower(strings.TrimSpace(value))
			if err := validate(value); err != nil {
				return err
			}
			return configfile.WriteUISection(path, section, map[string]string{key: value})
		},
		choices: choices,
	}
}

func searchBoundSetting(name, description, key, arg string) savedSetting {
	return savedSetting{
		name: name, short: description, arg: arg,
		get: func(path string) (string, error) {
			return readDisplayPreference(path, "search", key, "none")
		},
		set: func(path, value string) error {
			value = strings.TrimSpace(value)
			if strings.EqualFold(value, "none") {
				value = ""
			}
			return configfile.UpdateUISection(path, "search", func(fields configfile.Object) (bool, error) {
				encoded, err := json.Marshal(value)
				if err != nil {
					return false, err
				}
				fields[key] = encoded
				return true, validateSavedSearchBounds(fields)
			})
		},
	}
}

func readDisplayPreference(path, section, key, fallback string) (string, error) {
	raw, err := configfile.ReadUIPreference(path, section)
	if err != nil {
		return "", err
	}
	fields := configfile.Object{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return "", err
		}
	}
	value, err := readDisplayField(fields, key)
	if value == "" {
		value = fallback
	}
	return value, err
}

func readDisplayField(fields configfile.Object, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	return value, nil
}

func validateSavedSearchBounds(fields configfile.Object) error {
	var bounds searchresults.Filter
	for _, field := range []struct {
		key string
		dst **int64
	}{
		{"min_seeders", &bounds.MinSeeders}, {"min_size", &bounds.MinSize}, {"max_size", &bounds.MaxSize},
	} {
		raw, err := readDisplayField(fields, field.key)
		if err != nil {
			return err
		}
		if raw == "" {
			continue
		}
		var value int64
		if field.key == "min_seeders" {
			value, err = strconv.ParseInt(raw, 10, 64)
		} else {
			value, err = searchresults.ParseSize(raw)
		}
		if err != nil {
			return fmt.Errorf("invalid %s: %w", field.key, err)
		}
		*field.dst = &value
	}
	return bounds.Validate()
}
