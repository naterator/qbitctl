//go:build windows

package configfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

// acquireConfigLock uses a named kernel mutex. Windows releases ownership
// after a process crash, and the name is derived from the canonical config
// path so no lock file is left on disk.
func acquireConfigLock(path string) (func(), error) {
	// Windows mutex ownership belongs to an OS thread, not a goroutine. The
	// caller releases this lock synchronously from the same goroutine, so pin
	// that goroutine until the returned release function runs.
	path = strings.ToLower(filepath.Clean(path))
	digest := sha256.Sum256([]byte(path))
	name, err := windows.UTF16PtrFromString("Local\\qbitctl-config-" + hex.EncodeToString(digest[:]))
	if err != nil {
		return nil, fmt.Errorf("name config lock: %w", err)
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, fmt.Errorf("create config lock: %w", err)
	}
	if handle == 0 {
		return nil, errors.New("create config lock: returned an invalid handle")
	}

	runtime.LockOSThread()
	result, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	if err != nil {
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("wait for config lock: %w", err)
	}
	if result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED {
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("wait for config lock: unexpected result %#x", result)
	}

	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
	}, nil
}
