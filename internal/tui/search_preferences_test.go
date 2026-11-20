//go:build !plan9

package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTUIPreferencesDirUsesXDGConfigHome(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	got, err := tuiPreferencesDir()
	if err != nil {
		t.Fatalf("tuiPreferencesDir returned error: %v", err)
	}
	want := filepath.Join(configHome, "qbitctl")
	if got != want {
		t.Fatalf("tuiPreferencesDir = %q, want %q", got, want)
	}
}

func TestTUIPreferencesDirUsesHomeWhenXDGIsUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := tuiPreferencesDir()
	if err != nil {
		t.Fatalf("tuiPreferencesDir returned error: %v", err)
	}
	want := filepath.Join(home, ".config", "qbitctl")
	if got != want {
		t.Fatalf("tuiPreferencesDir = %q, want %q", got, want)
	}
}

func TestLoadSearchPreferencesMissingDefaults(t *testing.T) {
	order, filter, err := loadSearchPreferences(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("loadSearchPreferences returned error for missing file: %v", err)
	}
	if order != resultOriginal || filter != (resultFilter{}) {
		t.Fatalf("missing preferences = (%q, %+v), want defaults", order, filter)
	}
}

func TestLoadSearchPreferencesCorruptAndInvalidData(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: `{"sort":`},
		{name: "unknown sort", data: `{"sort":"not-a-sort"}`},
		{name: "invalid seeders", data: `{"sort":"size-desc","min_seeders":"-1"}`},
		{name: "invalid range", data: `{"sort":"size-desc","min_size":"4 GiB","max_size":"2 GiB"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tui-search.json")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			order, filter, err := loadSearchPreferences(path)
			if err == nil {
				t.Fatal("loadSearchPreferences returned nil error")
			}
			if order != resultOriginal || filter != (resultFilter{}) {
				t.Fatalf("invalid preferences = (%q, %+v), want defaults", order, filter)
			}
		})
	}
}

func TestLoadSearchPreferencesOptionalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui-search.json")
	if err := os.WriteFile(path, []byte(`{"sort":"size-asc"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	order, filter, err := loadSearchPreferences(path)
	if err != nil {
		t.Fatalf("loadSearchPreferences returned error: %v", err)
	}
	if order != resultSizeAsc || filter != (resultFilter{}) {
		t.Fatalf("optional preferences = (%q, %+v), want size-asc and empty bounds", order, filter)
	}
}

func TestSearchPreferencesRoundTripExcludesQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "tui-search.json")
	filter, err := parseResultFilter([len(resultFilterFields)]string{"Ubuntu 24.04", "25", "500 MB", "4 GiB"})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSearchPreferences(path, resultSeedersDesc, filter); err != nil {
		t.Fatalf("saveSearchPreferences returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("preferences mode = %04o, want 0600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); runtime.GOOS != "windows" && got != 0o700 {
		t.Fatalf("preferences directory mode = %04o, want 0700", got)
	}

	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), "Ubuntu 24.04") || strings.Contains(string(disk), "query") {
		t.Fatalf("saved preferences leaked query: %s", disk)
	}
	var fields map[string]any
	if err := json.Unmarshal(disk, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["sort"]; !ok {
		t.Fatalf("saved preferences omitted sort: %s", disk)
	}
	for _, field := range []string{"min_seeders", "min_size", "max_size"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("saved preferences omitted %s: %s", field, disk)
		}
	}

	order, loaded, err := loadSearchPreferences(path)
	if err != nil {
		t.Fatalf("loadSearchPreferences returned error: %v", err)
	}
	if order != resultSeedersDesc {
		t.Fatalf("loaded order = %q, want %q", order, resultSeedersDesc)
	}
	if loaded.query != "" {
		t.Fatalf("loaded query = %q, want empty", loaded.query)
	}
	if loaded.minSeeders.text != filter.minSeeders.text || loaded.minSeeders.value != filter.minSeeders.value ||
		loaded.minSize.text != filter.minSize.text || loaded.minSize.value != filter.minSize.value ||
		loaded.maxSize.text != filter.maxSize.text || loaded.maxSize.value != filter.maxSize.value {
		t.Fatalf("loaded bounds = %+v, want %+v", loaded, filter)
	}
}

func TestSaveSearchPreferencesPreservesExistingDirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tui-search.json")
	if err := saveSearchPreferences(path, resultOriginal, resultFilter{}); err != nil {
		t.Fatalf("saveSearchPreferences returned error: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("existing preferences directory mode = %04o, want 0750", got)
	}
}

func TestSaveSearchPreferencesRejectsInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui-search.json")
	if err := saveSearchPreferences(path, resultSort("unknown"), resultFilter{}); err == nil {
		t.Fatal("saveSearchPreferences accepted an unknown sort")
	}
	if err := saveSearchPreferences(path, resultOriginal, resultFilter{
		minSize: resultBound{text: "4 GiB", value: 4 << 30},
		maxSize: resultBound{text: "2 GiB", value: 2 << 30},
	}); err == nil {
		t.Fatal("saveSearchPreferences accepted an invalid range")
	}
}
