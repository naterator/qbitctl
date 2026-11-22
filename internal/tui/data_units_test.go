//go:build !plan9

package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestDataUnitsLoadAndExplicitOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := client.SetDataUnits(path, client.DataUnitsBytes); err != nil {
		t.Fatal(err)
	}

	m := newModel(t.Context(), nil)
	m.loadPreferences(path)
	if m.dataUnits != client.DataUnitsBytes || m.dataUnitsLabel() != "Bytes" {
		t.Fatalf("loaded units = %q (%s), want bytes", m.dataUnits, m.dataUnitsLabel())
	}

	explicit := newModel(t.Context(), nil)
	explicit.dataUnitsExplicit = true
	explicit.loadPreferences(path)
	if explicit.dataUnits != client.DataUnitsHuman {
		t.Fatalf("explicit human units were overridden by config: %q", explicit.dataUnits)
	}
}

func TestDataUnitsTogglePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := newModel(t.Context(), nil)
	m.configPath = path
	cmd := m.toggleDataUnits()
	if cmd == nil {
		t.Fatal("toggle did not start persistence")
	}
	done, ok := cmd().(workDone)
	if !ok {
		t.Fatalf("toggle command returned %T, want workDone", done)
	}
	m.finishWork(done)
	if m.dataUnits != client.DataUnitsBytes {
		t.Fatalf("effective units = %q, want bytes", m.dataUnits)
	}
	if got, err := client.ReadDataUnits(path); err != nil || got != client.DataUnitsBytes {
		t.Fatalf("saved units = %q, %v; want bytes", got, err)
	}
}

func TestDataUnitsFormatQueueSearchAndFilterSummary(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.dataUnits = client.DataUnitsHuman
	if got := m.viewSize(4 << 30); got != "4.0 GiB" {
		t.Fatalf("human size = %q", got)
	}
	if got := m.viewRate(4 << 30); got != "4.0 GiB/s" {
		t.Fatalf("human rate = %q", got)
	}
	m.resultFilter = resultFilter{minSize: resultBound{text: "4 GiB", value: 4 << 30}}
	if got := ansi.Strip(m.viewResultControls()); !strings.Contains(got, "size ≥ 4.0 GiB") {
		t.Fatalf("human filter summary = %q", got)
	}

	m.applyDataUnits(client.DataUnitsBytes)
	if got := m.viewSize(4 << 30); got != "4294967296 B" {
		t.Fatalf("byte size = %q", got)
	}
	if got := m.viewRate(4 << 30); got != "4294967296 B/s" {
		t.Fatalf("byte rate = %q", got)
	}
	if got := ansi.Strip(m.viewResultControls()); !strings.Contains(got, "size ≥ 4294967296 B") {
		t.Fatalf("byte filter summary = %q", got)
	}
}

func TestDataUnitsSearchColumnFitsByteCount(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.dataUnits = client.DataUnitsBytes
	line := ansi.Strip(m.viewSearchRow(client.SearchResult{Name: "Ubuntu", Size: 4294967296, Seeders: 3}, false, 76))
	if !strings.Contains(line, "4294967296 B") {
		t.Fatalf("search row clipped byte size: %q", line)
	}
}

func TestDataUnitsSearchColumnFitsMaxInt64AtNormalWidth(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.dataUnits = client.DataUnitsBytes
	const maxInt64 = int64(1<<63 - 1)
	line := ansi.Strip(m.viewSearchRow(client.SearchResult{
		Name: "Large archive",
		Size: maxInt64,
	}, false, 80))
	want := "9223372036854775807 B"
	if !strings.Contains(line, want) {
		t.Fatalf("search row clipped maximum byte size: %q, want %q", line, want)
	}
}

func TestDataUnitsBackendCloneDoesNotRaceModeToggle(t *testing.T) {
	backend := &tuiClient{app: &client.App{DataUnits: client.DataUnitsHuman}}
	backend.setDataUnits(client.DataUnitsBytes)
	clone := backend.withContext(context.Background(), nil, nil)
	if clone.DataUnits != client.DataUnitsBytes {
		t.Fatalf("backend clone units = %q, want bytes", clone.DataUnits)
	}
}
