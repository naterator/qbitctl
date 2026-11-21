// Package configfile provides small, transactional updates for qbitctl's
// shared JSON configuration file. It stores values as raw JSON so callers can
// add sections without this package needing to know their schema.
package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Object is the top-level JSON object in config.json.
type Object map[string]json.RawMessage

// Child decodes a nested JSON object. Missing keys return an empty object.
// Present null values and other JSON types return an error so callers cannot
// silently replace a malformed settings section.
func (o Object) Child(key string) (Object, error) {
	value, ok := o[key]
	if !ok {
		return Object{}, nil
	}
	var child Object
	if err := json.Unmarshal(value, &child); err != nil {
		return nil, fmt.Errorf("decode config section %q: %w", key, err)
	}
	if child == nil {
		return nil, fmt.Errorf("config section %q must be an object", key)
	}
	return child, nil
}

var processLock sync.Mutex

// Read loads path as a JSON object. A missing file is treated as an empty
// object, which lets a first-run config update create the file naturally.
func Read(path string) (Object, error) {
	path, err := configPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Object{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return decode(path, data)
}

// Update reads the latest object, gives it to change, and atomically writes it
// back when change reports true. A process and cross-process lock covers the
// read/modify/write sequence so concurrent settings changes do not overwrite
// one another.
func Update(path string, change func(Object) (bool, error)) error {
	if change == nil {
		return errors.New("config update callback is nil")
	}
	path, err := configPath(path)
	if err != nil {
		return err
	}

	processLock.Lock()
	defer processLock.Unlock()

	// The lock is held on the containing directory so it has no sidecar file
	// to become stale after a process crash. Creating the directory here also
	// lets a first-run update acquire the lock before the first config write.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", filepath.Dir(path), err)
	}
	unlock, err := acquireConfigLock(path)
	if err != nil {
		return err
	}
	defer unlock()

	object, err := Read(path)
	if err != nil {
		return err
	}
	write, err := change(object)
	if err != nil {
		return err
	}
	if !write {
		return nil
	}
	return writeObject(path, object)
}

func configPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("config path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	// Rename replaces a symlink instead of its target. Resolve an existing
	// symlink before updates so the established config path remains a link.
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, evalErr := filepath.EvalSymlinks(absolute)
		if evalErr == nil {
			return filepath.Abs(resolved)
		}
		// EvalSymlinks requires the destination to exist. A first write through
		// a symlink can legitimately create that destination, so resolve one
		// relative link ourselves when the target is not present yet.
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return "", fmt.Errorf("resolve config symlink %q: %w", absolute, readErr)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(absolute), target)
		}
		return filepath.Abs(target)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect config path %q: %w", absolute, statErr)
	}
	return absolute, nil
}

func decode(path string, data []byte) (Object, error) {
	var object Object
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("decode config %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode config %q: multiple JSON values", path)
		}
		return nil, fmt.Errorf("decode config %q: %w", path, err)
	}
	if object == nil {
		return nil, fmt.Errorf("decode config %q: top-level value must be an object", path)
	}
	return object, nil
}

func writeObject(path string, object Object) error {
	if object == nil {
		object = Object{}
	}
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config %q: %w", path, err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create config temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect config temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync config temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	removeTemp = false
	return syncDirectory(dir)
}
