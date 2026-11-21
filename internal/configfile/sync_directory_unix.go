//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package configfile

import (
	"fmt"
	"os"
)

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config directory for sync %q: %w", path, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync config directory %q: %w", path, err)
	}
	return nil
}
