//go:build !plan9

package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func tuiPreferencesDir() (string, error) {
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
	return filepath.Join(base, "qbitctl"), nil
}

func readPreferences(path string, disk any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read preferences: %w", err)
	}
	if err := json.Unmarshal(data, disk); err != nil {
		return fmt.Errorf("decode preferences: %w", err)
	}
	return nil
}

func writePreferences(path string, disk any) error {
	if path == "" {
		return errors.New("invalid preferences path")
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

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
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
