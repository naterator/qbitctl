package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	AppDir = "qbitctl"
	Name   = "config.json"
)

// DefaultPath is the primary config location, including the XDG override.
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return Name
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, AppDir, Name)
}

func SearchPaths() []string {
	primary := DefaultPath()
	if primary == Name {
		return []string{primary}
	}
	return []string{primary, Name}
}

// ResolvePath selects the config for setup and global preferences without
// inspecting credentials or profiles. An explicit path may be a save target.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, path := range SearchPaths() {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return "", fmt.Errorf("config path %q is a directory", path)
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect config path %q: %w", path, err)
		}
	}
	return DefaultPath(), nil
}
