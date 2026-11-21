//go:build !plan9

package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestQueuePreferencesRoundTripUsesStableSortIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qbitctl", "config.json")
	tests := []struct {
		order queueSort
		id    string
	}{
		{queueActiveFirst, "active-first"},
		{queueNameAsc, "name-asc"},
		{queueNameDesc, "name-desc"},
		{queueProgressAsc, "progress-asc"},
		{queueProgressDesc, "progress-desc"},
		{queueStateAsc, "state-asc"},
		{queueStateDesc, "state-desc"},
	}
	for _, tt := range tests {
		t.Run(string(tt.order), func(t *testing.T) {
			if err := saveQueuePreferences(path, tt.order); err != nil {
				t.Fatalf("saveQueuePreferences returned error: %v", err)
			}

			disk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := configStringField(t, disk, "ui", "queue", "sort"); got != tt.id {
				t.Fatalf("saved queue sort = %q, want stable ID %q", got, tt.id)
			}
			if configHasField(t, disk, "sort") {
				t.Fatal("queue sort leaked into the config root")
			}

			got, err := loadQueuePreferences(path)
			if err != nil {
				t.Fatalf("loadQueuePreferences returned error: %v", err)
			}
			if got != tt.order {
				t.Fatalf("loaded sort = %q, want %q", got, tt.order)
			}
		})
	}
}

func TestLoadQueuePreferencesMissingMalformedAndInvalidDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.json")
	got, err := loadQueuePreferences(missing)
	if err != nil {
		t.Fatalf("missing preferences returned error: %v", err)
	}
	if got != queueActiveFirst {
		t.Fatalf("missing preferences sort = %q, want %q", got, queueActiveFirst)
	}

	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: `{"ui":{"queue":{"sort":`},
		{name: "unknown sort", data: `{"ui":{"queue":{"sort":"not-a-sort"}}}`},
		{name: "missing sort", data: `{"ui":{"queue":{"other":"value"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := loadQueuePreferences(path)
			if tt.name == "missing sort" {
				if err != nil {
					t.Fatalf("optional sort returned error: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid preferences returned nil error")
			}
			if got != queueActiveFirst {
				t.Fatalf("invalid preferences sort = %q, want %q", got, queueActiveFirst)
			}
		})
	}
}

func TestQueuePreferencesPersistAcrossRestartAndPreserveSearchPreferences(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	searchFilter := resultFilter{}
	if err := saveSearchPreferences(configPath, resultSizeAsc, &searchFilter); err != nil {
		t.Fatalf("saveSearchPreferences returned error: %v", err)
	}
	searchBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	first := newModel(t.Context(), nil)
	first.loadPreferences(configPath)
	first.setQueueSort(queueProgressDesc)
	first.replaceQueue(queueTestTorrents())
	if got, want := queueTestHashes(first), []string{"z", "c", "a", "b", "d"}; !slices.Equal(got, want) {
		t.Fatalf("first queue order = %v, want %v", got, want)
	}
	if first.resultSort != resultSizeAsc {
		t.Fatalf("first search sort = %q, want %q", first.resultSort, resultSizeAsc)
	}

	second := newModel(t.Context(), nil)
	second.loadPreferences(configPath)
	second.replaceQueue(queueTestTorrents())
	if second.queueSort != queueProgressDesc {
		t.Fatalf("restarted queue sort = %q, want %q", second.queueSort, queueProgressDesc)
	}
	if got, want := queueTestHashes(second), []string{"z", "c", "a", "b", "d"}; !slices.Equal(got, want) {
		t.Fatalf("restarted queue order = %v, want %v", got, want)
	}
	if second.resultSort != resultSizeAsc {
		t.Fatalf("restarted search sort = %q, want %q", second.resultSort, resultSizeAsc)
	}

	// Selecting the default Active-first order is a real preference change and
	// must be persisted so a later restart does not retain the old sort.
	second.setQueueSort(queueActiveFirst)
	third := newModel(t.Context(), nil)
	third.loadPreferences(configPath)
	if third.queueSort != queueActiveFirst {
		t.Fatalf("reset queue sort = %q, want %q", third.queueSort, queueActiveFirst)
	}

	searchAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if configStringField(t, searchAfter, "ui", "search", "sort") != string(resultSizeAsc) {
		t.Fatalf("queue preference updates changed search sort: %s", searchAfter)
	}
	if configStringField(t, searchBefore, "ui", "search", "sort") != configStringField(t, searchAfter, "ui", "search", "sort") {
		t.Fatalf("queue preference updates changed search preferences: before %s, after %s", searchBefore, searchAfter)
	}
}

func configObject(t *testing.T, data []byte, keys ...string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(value, &object); err != nil {
			t.Fatalf("config field %q is not an object: %v", key, err)
		}
	}
	return object
}

func configStringField(t *testing.T, data []byte, keys ...string) string {
	t.Helper()
	if len(keys) == 0 {
		t.Fatal("configStringField requires a key")
	}
	object := configObject(t, data, keys[:len(keys)-1]...)
	if object == nil {
		return ""
	}
	value, ok := object[keys[len(keys)-1]]
	if !ok {
		return ""
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		t.Fatalf("config field %q is not a string: %v", keys[len(keys)-1], err)
	}
	return result
}

func configHasField(t *testing.T, data []byte, keys ...string) bool {
	t.Helper()
	if len(keys) == 0 {
		return false
	}
	object := configObject(t, data, keys[:len(keys)-1]...)
	if object == nil {
		return false
	}
	_, ok := object[keys[len(keys)-1]]
	return ok
}

func TestMigrateLegacyPreferencesFoldsIntoConfigAndRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(dir, "legacy")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	initial := `{"url":"http://qbittorrent","unknown":{"keep":true},"ui":{"queue":{"sort":"name-asc"}}}`
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, queuePreferencesFileName), []byte(`{"sort":"progress-desc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, searchPreferencesFileName), []byte(`{"sort":"size-asc","min_seeders":"25","min_size":"500 MB","max_size":"4 GiB"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migratePreferences(configPath, legacyDir); err != nil {
		t.Fatalf("migratePreferences returned error: %v", err)
	}
	for _, name := range []string{queuePreferencesFileName, searchPreferencesFileName} {
		if _, err := os.Stat(filepath.Join(legacyDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy preference %s still exists, stat error = %v", name, err)
		}
	}
	disk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := configStringField(t, disk, "url"); got != "http://qbittorrent" {
		t.Fatalf("auth config changed during migration: %s", disk)
	}
	if !configHasField(t, disk, "unknown", "keep") {
		t.Fatalf("unknown config field was lost during migration: %s", disk)
	}
	if got := configStringField(t, disk, "ui", "queue", "sort"); got != string(queueNameAsc) {
		t.Fatalf("existing queue sort was overwritten: %q", got)
	}
	if got := configStringField(t, disk, "ui", "search", "sort"); got != string(resultSizeAsc) {
		t.Fatalf("legacy search sort was not folded in: %q", got)
	}
	if got := configStringField(t, disk, "ui", "search", "min_seeders"); got != "25" {
		t.Fatalf("legacy search bounds were not folded in: %q", got)
	}
}

func TestMigrateLegacyPreferencesKeepsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(dir, "legacy")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"url":"http://qbittorrent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(legacyDir, queuePreferencesFileName)
	searchPath := filepath.Join(legacyDir, searchPreferencesFileName)
	if err := os.WriteFile(queuePath, []byte(`{"sort":"state-desc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(searchPath, []byte(`{"sort":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migratePreferences(configPath, legacyDir); err == nil {
		t.Fatal("migratePreferences accepted malformed legacy preferences")
	}
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Fatalf("valid legacy queue preference was not removed, stat error = %v", err)
	}
	if _, err := os.Stat(searchPath); err != nil {
		t.Fatalf("malformed legacy search preference was removed: %v", err)
	}
	disk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := configStringField(t, disk, "ui", "queue", "sort"); got != string(queueStateDesc) {
		t.Fatalf("valid legacy queue preference was not folded in: %q", got)
	}
}
