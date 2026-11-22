package main

import (
	"os"
	"path/filepath"
)

// removePreviousExecutable deletes the backup left by a Windows self-update.
// It is best effort: the previous process may still be exiting, in which case
// the next launch retries.
func removePreviousExecutable(goos string, executable func() (string, error), remove func(string) error) {
	if goos != "windows" {
		return
	}
	exePath, err := executable()
	if err != nil {
		return
	}
	if resolvedPath, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
		exePath = resolvedPath
	}
	oldPath := exePath + ".old"
	info, err := os.Lstat(oldPath)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if remove != nil {
		_ = remove(oldPath)
	}
}
