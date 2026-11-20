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
	path := filepath.Join(t.TempDir(), "qbitctl", "tui-queue.json")
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
			var fields map[string]string
			if err := json.Unmarshal(disk, &fields); err != nil {
				t.Fatalf("saved preferences are not JSON: %v", err)
			}
			if fields["sort"] != tt.id {
				t.Fatalf("saved sort = %q, want stable ID %q", fields["sort"], tt.id)
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
	missing := filepath.Join(t.TempDir(), "tui-queue.json")
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
		{name: "malformed JSON", data: `{"sort":`},
		{name: "unknown sort", data: `{"sort":"not-a-sort"}`},
		{name: "missing sort", data: `{"other":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tui-queue.json")
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
	queuePath := filepath.Join(dir, "tui-queue.json")
	searchPath := filepath.Join(dir, "tui-search.json")

	searchFilter := resultFilter{}
	if err := saveSearchPreferences(searchPath, resultSizeAsc, searchFilter); err != nil {
		t.Fatalf("saveSearchPreferences returned error: %v", err)
	}
	searchBefore, err := os.ReadFile(searchPath)
	if err != nil {
		t.Fatal(err)
	}

	first := newModel(t.Context(), nil)
	first.loadQueuePreferences(queuePath)
	first.loadSearchPreferences(searchPath)
	first.setQueueSort(queueProgressDesc)
	first.replaceQueue(queueTestTorrents())
	if got, want := queueTestHashes(first), []string{"z", "c", "a", "b", "d"}; !slices.Equal(got, want) {
		t.Fatalf("first queue order = %v, want %v", got, want)
	}
	if first.resultSort != resultSizeAsc {
		t.Fatalf("first search sort = %q, want %q", first.resultSort, resultSizeAsc)
	}

	second := newModel(t.Context(), nil)
	second.loadQueuePreferences(queuePath)
	second.loadSearchPreferences(searchPath)
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
	third.loadQueuePreferences(queuePath)
	if third.queueSort != queueActiveFirst {
		t.Fatalf("reset queue sort = %q, want %q", third.queueSort, queueActiveFirst)
	}

	searchAfter, err := os.ReadFile(searchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(searchBefore, searchAfter) {
		t.Fatalf("queue preference updates changed search preferences: before %s, after %s", searchBefore, searchAfter)
	}
}
