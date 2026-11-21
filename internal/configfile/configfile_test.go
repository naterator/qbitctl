package configfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReadMissingAndUpdatePreservesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	object, err := Read(path)
	if err != nil || len(object) != 0 {
		t.Fatalf("Read missing = %#v, %v", object, err)
	}
	if err := Update(path, func(object Object) (bool, error) {
		object["url"] = json.RawMessage("\"http://localhost:8080\"")
		object["owner"] = json.RawMessage("{\"name\":\"nate\"}")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(object Object) (bool, error) {
		object["ui"] = json.RawMessage("{\"queue\":{\"sort\":\"progress-desc\"}}")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"owner\"") || !strings.Contains(string(data), "\"ui\"") {
		t.Fatalf("unknown fields were lost: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %04o, want 0600", mode)
	}
}

func TestUpdateNoWriteAndMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"url\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(Object) (bool, error) {
		t.Fatal("callback called")
		return false, nil
	}); err == nil {
		t.Fatal("malformed config accepted")
	}
	if err := os.WriteFile(path, []byte("{\"keep\":true}"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := Update(path, func(Object) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("no-op update rewrote config: %q != %q", before, after)
	}
}

func TestUpdateSerializesConcurrentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if err := Update(path, func(object Object) (bool, error) {
				object["field"] = json.RawMessage("\"" + string(rune('a'+i)) + "\"")
				return true, nil
			}); err != nil {
				t.Errorf("Update(%d): %v", i, err)
			}
		}(i)
	}
	group.Wait()
	if _, err := Read(path); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePropagatesCallbackError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := errors.New("stop")
	if err := Update(path, func(Object) (bool, error) { return false, want }); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("callback error created config: %v", err)
	}
}

func TestUpdateFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Update(link, func(object Object) (bool, error) {
		object["ui"] = json.RawMessage("{}")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config symlink replaced: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePreservesWhitespaceInPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), " config.json ")
	if err := Update(path, func(object Object) (bool, error) {
		object["literal"] = json.RawMessage("true")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("literal path was not written: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected lock sidecar: %v", err)
	}
}

func TestChild(t *testing.T) {
	object := Object{
		"nested": json.RawMessage(`{"sort":"progress-desc"}`),
		"null":   json.RawMessage(`null`),
		"array":  json.RawMessage(`[]`),
	}
	child, err := object.Child("nested")
	if err != nil || string(child["sort"]) != `"progress-desc"` {
		t.Fatalf("Child(nested) = %#v, %v", child, err)
	}
	missing, err := object.Child("missing")
	if err != nil || len(missing) != 0 {
		t.Fatalf("Child(missing) = %#v, %v", missing, err)
	}
	if _, err := object.Child("null"); err == nil {
		t.Fatal("Child(null) unexpectedly succeeded")
	}
	if _, err := object.Child("array"); err == nil {
		t.Fatal("Child(array) unexpectedly succeeded")
	}
}
