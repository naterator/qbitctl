package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePreviousExecutableRemovesWindowsBackup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "qbitctl.exe")
	old := exe + ".old"
	for _, path := range []string{exe, old} {
		if err := os.WriteFile(path, []byte(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	removePreviousExecutable("windows", func() (string, error) {
		return exe, nil
	}, os.Remove)

	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup stat error = %v, want file removed", err)
	}
}

func TestRemovePreviousExecutableResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "qbitctl.exe")
	link := filepath.Join(dir, "qbitctl.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".old", []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	removePreviousExecutable("windows", func() (string, error) {
		return link, nil
	}, os.Remove)

	if _, err := os.Stat(target + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolved backup stat error = %v, want file removed", err)
	}
}

func TestRemovePreviousExecutableIgnoresNonWindows(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "qbitctl.exe")
	old := exe + ".old"
	if err := os.WriteFile(old, []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	called := false
	removePreviousExecutable("linux", func() (string, error) {
		t.Fatal("executable should not be called on non-Windows")
		return "", nil
	}, func(string) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("remove called on non-Windows")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("backup stat error = %v, want file preserved", err)
	}
}

func TestRemovePreviousExecutableIgnoresErrors(t *testing.T) {
	executableErr := errors.New("executable unavailable")
	removeCalls := 0
	removePreviousExecutable("windows", func() (string, error) {
		return "", executableErr
	}, func(string) error {
		removeCalls++
		return errors.New("remove failed")
	})
	if removeCalls != 0 {
		t.Fatalf("remove calls after executable error = %d, want 0", removeCalls)
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "qbitctl.exe")
	if err := os.WriteFile(exe+".old", []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	removePreviousExecutable("windows", func() (string, error) {
		return exe, nil
	}, func(path string) error {
		removeCalls++
		if path != exe+".old" {
			t.Fatalf("remove path = %q", path)
		}
		return errors.New("remove failed")
	})
	if removeCalls != 1 {
		t.Fatalf("remove calls after remove error = %d, want 1", removeCalls)
	}
}

func TestRemovePreviousExecutablePreservesNonRegularBackups(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "qbitctl.exe")
		old := exe + ".old"
		if err := os.Mkdir(old, 0o700); err != nil {
			t.Fatal(err)
		}
		removePreviousExecutable("windows", func() (string, error) {
			return exe, nil
		}, func(string) error {
			t.Fatal("remove called for backup directory")
			return nil
		})
		if info, err := os.Stat(old); err != nil || !info.IsDir() {
			t.Fatalf("backup directory stat = %v, info = %#v", err, info)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "qbitctl.exe")
		old := exe + ".old"
		target := filepath.Join(dir, "previous.exe")
		if err := os.WriteFile(target, []byte("previous"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, old); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		removePreviousExecutable("windows", func() (string, error) {
			return exe, nil
		}, func(string) error {
			t.Fatal("remove called for backup symlink")
			return nil
		})
		info, err := os.Lstat(old)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("backup symlink stat = %v, info = %#v", err, info)
		}
	})
}
