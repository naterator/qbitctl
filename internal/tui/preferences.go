//go:build !plan9

package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/naterator/qbitctl/internal/configfile"
)

func tuiPreferencesDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "qbitctl"), nil
}

func (m *model) loadPreferences(path string) {
	m.preferencesPath = path
	if path == "" {
		return
	}
	if dir, err := tuiPreferencesDir(); err == nil && usesDefaultPreferences(path, dir) {
		m.preferenceNotice(migratePreferences(path, dir))
	}
	order, err := loadQueuePreferences(path)
	m.queueSort = order
	if err != nil {
		m.preferenceNotice(fmt.Errorf("queue preferences: %w", err))
	}
	sortOrder, filter, err := loadSearchPreferences(path)
	m.resultSort, m.resultFilter = sortOrder, filter
	if err != nil {
		m.preferenceNotice(fmt.Errorf("search preferences: %w", err))
	}
	m.loadTheme(path)
}

func usesDefaultPreferences(path, dir string) bool {
	active, activeErr := filepath.Abs(path)
	primary, primaryErr := filepath.Abs(filepath.Join(dir, "config.json"))
	if activeErr != nil || primaryErr != nil {
		return false
	}
	if active == primary {
		return true
	}
	local, localErr := filepath.Abs("config.json")
	if localErr != nil || active != local {
		return false
	}
	_, err := os.Stat(primary)
	return errors.Is(err, os.ErrNotExist)
}

func (m *model) preferenceNotice(err error) {
	if err == nil {
		return
	}
	message := err.Error()
	if m.notice != "" {
		message = m.notice + " · " + message
	}
	m.setNotice(message, true)
}

func readPreferences(path, section string) (json.RawMessage, error) {
	root, err := configfile.Read(path)
	if err != nil {
		return nil, err
	}
	ui, err := root.Child("ui")
	if err != nil {
		return nil, err
	}
	return ui[section], nil
}

func updatePreferences(path, section string, edit func(configfile.Object) (bool, error)) error {
	return configfile.Update(path, func(root configfile.Object) (bool, error) {
		ui, err := root.Child("ui")
		if err != nil {
			return false, err
		}
		fields, err := ui.Child(section)
		if err != nil {
			return false, err
		}
		changed, err := edit(fields)
		if err != nil || !changed {
			return false, err
		}
		update, err := json.Marshal(fields)
		if err != nil {
			return false, err
		}
		ui[section] = update
		root["ui"], err = json.Marshal(ui)
		return err == nil, err
	})
}

func writePreferences(path, section string, values map[string]string) error {
	return updatePreferences(path, section, func(fields configfile.Object) (bool, error) {
		changed := false
		for key, value := range values {
			raw, _ := json.Marshal(value)
			if !bytes.Equal(fields[key], raw) {
				fields[key] = raw
				changed = true
			}
		}
		return changed, nil
	})
}

func migratePreferences(path, legacyDir string) error {
	return errors.Join(
		migratePreferenceFile(path, "queue", filepath.Join(legacyDir, queuePreferencesFileName),
			func(raw json.RawMessage) error {
				_, err := decodeQueuePreferences(raw)
				return err
			}),
		migratePreferenceFile(path, "search", filepath.Join(legacyDir, searchPreferencesFileName),
			func(raw json.RawMessage) error {
				_, _, err := decodeSearchPreferences(raw)
				return err
			}),
	)
}

func migratePreferenceFile(path, section, legacyPath string, validate func(json.RawMessage) error) error {
	before, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(legacyPath), err)
	}
	var legacy configfile.Object
	if err := json.Unmarshal(before, &legacy); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(legacyPath), err)
	}
	if legacy == nil {
		return fmt.Errorf("%s must contain a JSON object", filepath.Base(legacyPath))
	}
	if err := validate(before); err != nil {
		return fmt.Errorf("invalid %s: %w", filepath.Base(legacyPath), err)
	}

	if err := updatePreferences(path, section, func(fields configfile.Object) (bool, error) {
		changed := false
		for key, value := range legacy {
			if _, exists := fields[key]; !exists {
				fields[key] = value
				changed = true
			}
		}
		merged, err := json.Marshal(fields)
		if err != nil {
			return false, err
		}
		return changed, validate(merged)
	}); err != nil {
		return fmt.Errorf("migrate %s: %w", filepath.Base(legacyPath), err)
	}

	return removeMigratedPreferences(legacyPath, before)
}

func removeMigratedPreferences(path string, migrated []byte) error {
	// A previous release may still have this file open for writes.
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify migrated %s: %w", filepath.Base(path), err)
	}
	if !bytes.Equal(current, migrated) {
		return fmt.Errorf("%s changed during migration", filepath.Base(path))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove migrated %s: %w", filepath.Base(path), err)
	}
	return nil
}
