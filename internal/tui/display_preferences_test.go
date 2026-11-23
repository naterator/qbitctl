//go:build !plan9

package tui

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestQueueColumnsAndRefreshPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveQueueColumns(path, queueColumnsWide); err != nil {
		t.Fatal(err)
	}
	if err := saveRefreshInterval(path, 23*time.Second); err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), nil)
	m.loadPreferences(path)
	if m.queueColumns != queueColumnsWide {
		t.Fatalf("queue columns = %q, want wide", m.queueColumns)
	}
	if m.refreshIntervalSetting != 23*time.Second {
		t.Fatalf("refresh interval = %s, want 23s", m.refreshIntervalSetting)
	}
	if got := m.refreshInterval(); got != 23*time.Second {
		t.Fatalf("refreshInterval() = %s, want 23s", got)
	}
	m.setRefreshInterval(0)
	if m.refreshIntervalSetting != 0 || m.refreshInterval() != activeRefreshInterval {
		t.Fatalf("adaptive refresh setting = %s / %s", m.refreshIntervalSetting, m.refreshInterval())
	}
}

func TestQueueColumnsKeyboardPickerPersistsWideChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := newModel(t.Context(), nil)
	m.page = queuePage
	m.preferencesPath = path
	if cmd := m.handleKey("c"); cmd != nil || m.dialog == nil || m.dialog.kind != dialogQueueColumns {
		t.Fatalf("queue columns key opened command=%v dialog=%#v", cmd != nil, m.dialog)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.dialog.selected != 1 {
		t.Fatalf("queue columns selection = %d, want wide option", m.dialog.selected)
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog != nil || m.queueColumns != queueColumnsWide {
		t.Fatalf("queue columns submit left dialog=%#v preset=%q", m.dialog, m.queueColumns)
	}
	if got, err := loadQueueColumns(path); err != nil || got != queueColumnsWide {
		t.Fatalf("saved queue columns = %q, err=%v; want wide", got, err)
	}
}

func TestRefreshIntervalDialogValidatesAndSavesInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := newModel(t.Context(), nil)
	m.preferencesPath = path
	m.openRefreshIntervalDialog()
	if m.dialog == nil || m.dialog.kind != dialogRefreshInterval {
		t.Fatalf("refresh dialog = %#v", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("250ms")
	m.submitDialog()
	if m.dialog == nil || !strings.Contains(m.dialog.status, "at least 1s") {
		t.Fatalf("short interval status = %q, want validation error", m.dialog.status)
	}
	m.dialog.fields[0].input.SetValue("23s")
	m.submitDialog()
	if m.dialog != nil || m.refreshIntervalSetting != 23*time.Second {
		t.Fatalf("valid interval submit left dialog=%#v interval=%s", m.dialog, m.refreshIntervalSetting)
	}
	if got, err := loadRefreshInterval(path); err != nil || got != 23*time.Second {
		t.Fatalf("saved refresh interval = %s, err=%v; want 23s", got, err)
	}
	m.openRefreshIntervalDialog()
	m.dialog.fields[0].input.SetValue("adaptive")
	m.submitDialog()
	if m.dialog != nil || m.refreshIntervalSetting != 0 {
		t.Fatalf("adaptive interval submit left dialog=%#v interval=%s", m.dialog, m.refreshIntervalSetting)
	}
}

func TestWideQueueColumnsRemainCompactAtNarrowWidths(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.queueColumns = queueColumnsWide
	narrow := ansi.Strip(m.viewQueueRow(client.TorrentInfo{
		Name: "Ubuntu", TotalSize: 4 << 30, Ratio: 1.25, ETA: 3600,
		Category: "linux", State: "downloading",
	}, true, 76))
	if strings.Contains(narrow, "Category") || strings.Contains(narrow, "Ratio") {
		t.Fatalf("narrow queue unexpectedly displayed wide-only fields: %q", narrow)
	}
	wide := ansi.Strip(m.viewQueueHeader(150))
	for _, want := range []string{"Size", "Ratio", "ETA", "Category", "Seeds"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide queue header omitted %q: %q", want, wide)
		}
	}
}

func TestSearchRowsShowLeechersOnlyWhenWide(t *testing.T) {
	m := newModel(t.Context(), nil)
	row := client.SearchResult{Name: "Ubuntu", Size: 4 << 30, Seeders: 4, Leechers: 9, Source: "example"}
	narrow := ansi.Strip(m.viewSearchRow(row, false, 76))
	wide := ansi.Strip(m.viewSearchRow(row, false, 100))
	if strings.Contains(narrow, "  9") {
		t.Fatalf("narrow search row unexpectedly displayed leechers: %q", narrow)
	}
	if !strings.Contains(wide, "9") {
		t.Fatalf("wide search row omitted leechers: %q", wide)
	}
}

func TestWriteOSC52SupportsDirectAndTmuxSequences(t *testing.T) {
	for _, tmux := range []bool{false, true} {
		var output bytes.Buffer
		if err := writeOSC52(&output, "abc", tmux); err != nil {
			t.Fatal(err)
		}
		encoded := base64.StdEncoding.EncodeToString([]byte("abc"))
		if !strings.Contains(output.String(), encoded) || !strings.Contains(output.String(), "52;c;") {
			t.Fatalf("OSC52 output = %q", output.String())
		}
	}
}

func TestCopyDialogCopiesSelectedTorrentHashAndMagnet(t *testing.T) {
	var output bytes.Buffer
	m := newModel(t.Context(), nil)
	m.output = &output
	m.page = queuePage
	m.replaceQueue([]client.TorrentInfo{{Hash: "0123456789abcdef0123456789abcdef01234567", Name: "Ubuntu ISO"}})
	m.openCopyDialog()
	if m.dialog == nil || len(m.dialog.items) != 2 {
		t.Fatalf("copy dialog = %#v, want hash and magnet choices", m.dialog)
	}
	m.dialog.selected = 0
	_ = m.updateDialog(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(output.String(), "52;c;") {
		t.Fatalf("copy did not emit OSC52: %q", output.String())
	}
}

func TestSearchCopyDialogOffersHashFromMagnet(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{{Name: "Ubuntu", URL: "magnet:?xt=urn:btih:abc123"}})
	m.openCopyDialog()
	if m.dialog == nil || len(m.dialog.items) != 2 || m.dialog.items[0].action != "hash" {
		t.Fatalf("search copy dialog = %#v, want hash and magnet choices", m.dialog)
	}
}
