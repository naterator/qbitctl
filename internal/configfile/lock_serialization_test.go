//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris || windows

package configfile

import (
	"path/filepath"
	"testing"
	"time"
)

// TestConfigLockSerializesSeparateCallers exercises the platform-specific
// process lock rather than the package mutex. This catches regressions where
// two processes could update the same config at once on Windows or Unix.
func TestConfigLockSerializesSeparateCallers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	firstHeld := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondAcquired := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		release, err := acquireConfigLock(path)
		if err != nil {
			firstDone <- err
			return
		}
		close(firstHeld)
		<-releaseFirst
		release()
		firstDone <- nil
	}()

	select {
	case err := <-firstDone:
		t.Fatalf("first lock: %v", err)
	case <-firstHeld:
	case <-time.After(2 * time.Second):
		t.Fatal("first lock was not acquired")
	}

	go func() {
		release, err := acquireConfigLock(path)
		if err == nil {
			close(secondAcquired)
			release()
		}
		secondDone <- err
	}()

	select {
	case <-secondAcquired:
		t.Fatal("second caller acquired the lock while the first held it")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first caller did not release the lock")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second caller did not acquire the lock after release")
	}
}
