//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireConfigLock takes an advisory lock on the containing directory. The
// descriptor remains open for the lifetime of the returned release function,
// so the kernel releases the lock if the process exits unexpectedly. Using the
// directory itself avoids leaving a lock sidecar next to the user's config.
func acquireConfigLock(path string) (func(), error) {
	dirPath := filepath.Dir(path)
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, fmt.Errorf("open config lock directory %q: %w", dirPath, err)
	}

	for {
		err = unix.Flock(int(dir.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("lock config directory %q: %w", dirPath, err)
	}

	return func() {
		_ = unix.Flock(int(dir.Fd()), unix.LOCK_UN)
		_ = dir.Close()
	}, nil
}
