//go:build !plan9

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCompactPaletteKeepsSelectionAndFilterVisible(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.width, m.height = 44, 10
	m.handleKey("ctrl+p")
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Wrap to the last command.

	frame := m.View().Content
	assertFrameSize(t, frame, m.width, m.height)
	for _, part := range []string{"Command palette", "Find a command", "› Quit", "esc close"} {
		if !strings.Contains(ansi.Strip(frame), part) {
			t.Errorf("compact command palette omitted %q", part)
		}
	}

	m.dialog.fields[0].input.SetValue("sort queue")
	m.dialog.refreshPalette()
	frame = m.View().Content
	if !strings.Contains(ansi.Strip(frame), "› Sort queue") {
		t.Fatal("filter did not focus its first matching command")
	}
}

func TestShortcutHelpScrollsInCompactTerminals(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.width, m.height = 42, 12
	m.handleKey("?")
	frame := m.View().Content
	assertFrameSize(t, frame, m.width, m.height)
	if !strings.Contains(ansi.Strip(frame), "Keyboard shortcuts") {
		t.Fatal("help title is missing")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	frame = m.View().Content
	if !strings.Contains(ansi.Strip(frame), "Cancel search / quit") {
		t.Fatal("End did not reveal the last group")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil {
		t.Fatal("Escape did not return to the workspace")
	}
}

func TestSurfacePreservesColorsAcrossInlineStyles(t *testing.T) {
	frame := viewSurface(viewKeyStyle.Render("a")+" add", 10, 2, viewPanelColor, viewTextStyle.GetForeground())
	canvas := lipgloss.NewCanvas(10, 2).Compose(lipgloss.NewLayer(frame))
	wantR, wantG, wantB, _ := viewPanelColor.RGBA()
	for y := range 2 {
		for x := range 10 {
			bg := canvas.CellAt(x, y).Style.Bg
			if bg == nil {
				t.Fatalf("cell (%d,%d) has no background", x, y)
			}
			r, g, b, _ := bg.RGBA()
			if r != wantR || g != wantG || b != wantB {
				t.Fatalf("cell (%d,%d) has wrong background: %v", x, y, bg)
			}
		}
	}
}

func TestDetailTableAlignsLabelsAndWrappedValues(t *testing.T) {
	path := "/downloads/" + strings.Repeat("large-directory/", 4) + "file.iso"
	rows := []detailRow{
		{label: "Name", value: "Sample torrent"},
		{label: "Full Path", value: path},
		{label: "Tags"},
	}
	const width = 42
	lines := dialogDetailLines(rows, width)
	var wrappedPath strings.Builder
	for i, line := range lines {
		plain := ansi.Strip(line)
		label, value, ok := strings.Cut(plain, "│ ")
		if !ok || lipgloss.Width(label) != len("Full Path")+2 || lipgloss.Width(line) > width {
			t.Fatalf("misaligned detail line %d: %q", i, plain)
		}
		switch i {
		case 0:
			if plain != "      Name │ Sample torrent" {
				t.Errorf("misaligned name row: %q", plain)
			}
		case len(lines) - 1:
			if strings.TrimSpace(label) != "Tags" || value != "—" {
				t.Errorf("empty field is unclear: %q", plain)
			}
		default:
			wrappedPath.WriteString(value)
		}
	}
	if got := wrappedPath.String(); got != path {
		t.Fatalf("wrapped path lost content: %q, want %q", got, path)
	}
}

func TestDetailDialogScrollsWhenColumnsStack(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.width, m.height = 34, 12
	m.openDetailDialog("Sample torrent", []detailRow{
		{label: "Full Path", value: "/downloads/" + strings.Repeat("large-directory/", 6) + "file.iso"},
		{label: "Transferred: Up", value: "123456789"},
	})
	frame := m.View().Content
	assertFrameSize(t, frame, m.width, m.height)
	if !strings.Contains(ansi.Strip(frame), "Full Path") {
		t.Fatal("first field missing from narrow detail dialog")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	frame = m.View().Content
	if !strings.Contains(ansi.Strip(frame), "123456789") {
		t.Fatal("End did not reveal the last value")
	}
}

func assertFrameSize(t *testing.T, frame string, width, height int) {
	t.Helper()
	if got := lipgloss.Height(frame); got != height {
		t.Errorf("frame height = %d, want %d", got, height)
	}
	for i, line := range strings.Split(frame, "\n") {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("line %d width = %d, want %d", i, got, width)
		}
	}
}
