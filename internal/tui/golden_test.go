//go:build !plan9

package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	"github.com/naterator/qbitctl/pkg/client"
)

var updateGoldenFrames = flag.Bool("update", false, "rewrite TUI golden frames")

type goldenCase struct {
	name  string
	setup func(*model)
}

func TestGoldenFrames(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(goldenConfigRoot(), "cache"))
	for _, tc := range goldenCases() {
		for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, size.width, size.height), func(t *testing.T) {
				m := newModel(t.Context(), nil)
				m.width, m.height = size.width, size.height
				tc.setup(m)
				assertGoldenFrame(t, fmt.Sprintf("%s-%dx%d", tc.name, m.width, m.height), m)
			})
		}
	}
}

func goldenConfigRoot() string {
	if runtime.GOOS == "windows" {
		return `C:\`
	}
	return "/t/"
}

func TestStyledGoldenThemes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	for _, theme := range []terminaltheme.ID{terminaltheme.Default, "high-contrast"} {
		t.Run(string(theme), func(t *testing.T) {
			m := newModel(t.Context(), nil)
			m.width, m.height, m.theme = 80, 24, theme
			goldenCases()[1].setup(m)
			frame := m.View().Content
			assertFrameSize(t, frame, m.width, m.height)
			if !strings.Contains(frame, "\x1b[") {
				t.Fatal("styled frame did not retain ANSI styling")
			}
			assertStyledGoldenFrame(t, "styled-"+string(theme)+"-80x24", frame)
		})
	}
}

func TestNormalizeGoldenFrameAcceptsCRLF(t *testing.T) {
	if got := normalizeGoldenFrame("one\r\ntwo\r\n"); got != "one\ntwo\n" {
		t.Fatalf("normalized CRLF frame = %q", got)
	}
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{name: "queue-empty", setup: func(*model) {}},
		{name: "queue-three-marked-unicode", setup: func(m *model) {
			m.replaceQueue([]client.TorrentInfo{
				{Name: "Ubuntu 24.04 LTS", Hash: "a", State: "downloading", Progress: .42, DLSpeed: 1024 * 1024},
				{Name: "日本語 🌐 release", Hash: "b", State: "stalledUP", Progress: 1, TotalSize: 8 * 1024 * 1024},
				{Name: strings.Repeat("long-directory-name/", 4) + "file.iso", Hash: "c", State: "error", Progress: .07},
			})
			m.toggleMark()
		}},
		{name: "queue-filtered-empty", setup: func(m *model) {
			m.replaceQueue([]client.TorrentInfo{{Name: "Ubuntu", Hash: "a", State: "downloading"}})
			m.setFilter("does-not-match", "all")
		}},
		{name: "queue-connecting", setup: func(m *model) {
			m.busy, m.work, m.connectTarget = "Connecting to qbt.test", connectWork, "qbt.test"
		}},
		{name: "queue-error", setup: func(m *model) {
			m.setNotice("connection failed", true)
		}},
		{name: "search-empty", setup: func(m *model) {
			m.page, m.query = searchPage, ""
		}},
		{name: "search-results-unknown-size", setup: func(m *model) {
			m.page, m.query = searchPage, "Ubuntu"
			m.replaceSearchResults([]client.SearchResult{
				{ID: 101, Name: "Ubuntu ISO", Size: -1, Seeders: 12, Leechers: 3, Source: "jackett"},
				{Name: "Unknown size result", Size: -1, Seeders: 0, Source: "fixture"},
			})
		}},
		{name: "search-filter-editor", setup: func(m *model) {
			m.page, m.query = searchPage, "Ubuntu"
			m.replaceSearchResults([]client.SearchResult{{ID: 101, Name: "Ubuntu ISO", Size: 4 * 1024 * 1024, Seeders: 12, Source: "jackett"}})
			m.startResultFilter()
		}},
		{name: "search-sort-dialog", setup: func(m *model) {
			m.page = searchPage
			m.openSortDialog(dialogResultSort)
		}},
		{name: "search-provider-picker", setup: func(m *model) {
			m.page = searchPage
			m.searchPlugins = []searchPluginInfo{
				{Name: "prowlarr", FullName: "Prowlarr", Enabled: true},
				{Name: "jackett", FullName: "Jackett", Enabled: false},
			}
			m.searchPluginsAttempted = true
			m.openSearchPluginPicker()
		}},
		{name: "server-populated", setup: func(m *model) {
			m.page = serverPage
			m.serverRows = []detailRow{{label: "Connection", value: "connected"}, {label: "Download speed", value: "1.2 MiB/s"}, {label: "DHT nodes", value: "42"}}
		}},
		{name: "server-error", setup: func(m *model) {
			m.page, m.failed = serverPage, true
			m.setNotice("server request failed", true)
		}},
		{name: "config-saved-password", setup: func(m *model) {
			m.page, m.setup = configPage, setupState{path: filepath.Join(goldenConfigRoot(), "qbitctl", "config.json"), url: "https://qbt.test/qbt", user: "admin", hasPassword: true, tuiDefault: true, ready: true}
		}},
		{name: "config-first-run-form", setup: func(m *model) {
			m.page, m.setup = configPage, setupState{path: filepath.Join(goldenConfigRoot(), "qbitctl", "config.json")}
			m.openConfigDialog()
		}},
		{name: "config-theme-picker", setup: func(m *model) {
			m.page = configPage
			m.openThemePicker()
		}},
		{name: "dialog-delete-two-marks", setup: func(m *model) {
			m.replaceQueue([]client.TorrentInfo{{Name: "one", Hash: "a"}, {Name: "two", Hash: "b"}})
			m.toggleMark()
			m.moveCursor(1)
			m.toggleMark()
			m.openRemoveDialog(true)
		}},
		{name: "dialog-edit-mixed", setup: func(m *model) {
			m.replaceQueue([]client.TorrentInfo{{Name: "one", Hash: "a", State: "downloading"}, {Name: "two", Hash: "b", State: "stalledUP"}})
			m.toggleMark()
			m.moveCursor(1)
			m.toggleMark()
			m.openEditDialog()
		}},
		{name: "dialog-help-scrolled", setup: func(m *model) {
			m.openHelpDialog()
			m.dialog.scroll = 8
		}},
		{name: "narrow-queue", setup: func(m *model) {
			m.width, m.height = 40, 12
			m.replaceQueue([]client.TorrentInfo{{Name: "日本語 🌐 long name", Hash: "a", State: "downloading", Progress: .5}})
		}},
	}
}

func assertGoldenFrame(t *testing.T, name string, m *model) {
	t.Helper()
	frame := m.View().Content
	assertFrameSize(t, frame, m.width, m.height)
	got := normalizeGoldenFrame(frame)
	if runtime.GOOS == "windows" && m.page == configPage {
		// Keep both fixture roots the same width before comparing path display.
		got = strings.ReplaceAll(filepath.ToSlash(got), "C:/", "/t/")
	}
	path := filepath.Join("testdata", "frames", name+".txt")
	if *updateGoldenFrames {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden frame %s: %v (run go test ./internal/tui -run Golden -update)", path, err)
	}
	want := normalizeGoldenFrame(string(wantBytes))
	if got != want {
		t.Fatalf("golden frame %s differs\n%s", name, goldenDiff(want, got))
	}
}

func normalizeGoldenFrame(frame string) string {
	frame = strings.ReplaceAll(ansi.Strip(frame), "\r\n", "\n")
	frame = strings.TrimRight(frame, " \n")
	lines := strings.Split(frame, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func assertStyledGoldenFrame(t *testing.T, name, frame string) {
	t.Helper()
	got := normalizeStyledGoldenFrame(frame)
	path := filepath.Join("testdata", "frames", name+".txt")
	if *updateGoldenFrames {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read styled golden frame %s: %v (run go test ./internal/tui -run StyledGolden -update)", path, err)
	}
	if got != normalizeStyledGoldenFrame(string(wantBytes)) {
		t.Fatalf("styled golden frame %s differs", name)
	}
}

func normalizeStyledGoldenFrame(frame string) string {
	frame = strings.ReplaceAll(frame, "\r\n", "\n")
	frame = strings.TrimRight(frame, " \n")
	lines := strings.Split(frame, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func goldenDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	limit := len(wantLines)
	if len(gotLines) > limit {
		limit = len(gotLines)
	}
	var diff strings.Builder
	for i := 0; i < limit; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&diff, "line %d\n- %s\n+ %s\n", i+1, w, g)
		}
	}
	return diff.String()
}
