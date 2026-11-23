//go:build !plan9

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// trackerService loads structured tracker entries when the Trackers tab is
// selected or refreshed.
type trackerService interface {
	trackers(context.Context, string) ([]client.TrackerEntry, error)
}

func (m *model) closeTrackerForm() {
	d := m.dialog
	if d == nil || (d.kind != dialogTrackerAdd && d.kind != dialogTrackerEdit) {
		return
	}
	d.kind = dialogDetails
	d.title, d.trackerTitle = d.trackerTitle, ""
	d.fields = nil
	d.focus = 0
	d.request = actionRequest{}
	d.status = ""
	m.ensureTrackerSelectionVisible()
}

func (m *model) replaceTrackerEntries(entries []client.TrackerEntry) {
	d := m.dialog
	if d == nil {
		return
	}
	oldSelected := d.selected
	oldURL := ""
	if oldSelected >= 0 && oldSelected < len(d.trackerEntries) {
		oldURL = d.trackerEntries[oldSelected].URL
	}
	d.trackerEntries = append([]client.TrackerEntry(nil), entries...)
	if len(d.trackerEntries) == 0 {
		d.selected = 0
		d.scroll = 0
		return
	}
	if oldURL != "" {
		for i, entry := range d.trackerEntries {
			if entry.URL == oldURL {
				d.selected = i
				m.ensureTrackerSelectionVisible()
				return
			}
		}
	}
	d.selected = min(max(0, oldSelected), len(d.trackerEntries)-1)
	m.ensureTrackerSelectionVisible()
}

func (m *model) updateTrackerTab(name string) tea.Cmd {
	d := m.dialog
	switch name {
	case "up", "k":
		m.stepTrackerSelection(-1)
	case "down", "j":
		m.stepTrackerSelection(1)
	case "pgup", "ctrl+u":
		m.stepTrackerSelection(-m.trackerVisibleRows())
	case "pgdown", "ctrl+d":
		m.stepTrackerSelection(m.trackerVisibleRows())
	case "home", "g":
		d.selected, d.scroll = 0, 0
	case "end", "G", "shift+g":
		d.selected = max(0, len(d.trackerEntries)-1)
		m.ensureTrackerSelectionVisible()
	case "v", "V":
		// Full URLs can include private tracker credentials.
		d.trackerReveal = !d.trackerReveal
	case "a", "A", "e", "E":
		if m.busy != "" {
			d.status = "Finish the current request first"
			return nil
		}
		if name == "a" || name == "A" {
			m.openTrackerAddDialog()
		} else {
			m.openTrackerEditDialog()
		}
	case "d", "D", "x", "X":
		return m.removeSelectedTracker()
	case "r", "R":
		return m.refreshTorrentDetails()
	}
	return nil
}

func (m *model) updateTrackerForm(name string, key tea.KeyPressMsg) tea.Cmd {
	d := m.dialog
	switch name {
	case "tab":
		return d.cycleField(false)
	case "shift+tab":
		return d.cycleField(true)
	case "enter", "return":
		return m.submitTrackerDialog()
	}
	if len(d.fields) == 0 || d.focus < 0 || d.focus >= len(d.fields) {
		return nil
	}
	var cmd tea.Cmd
	d.fields[d.focus].input, cmd = d.fields[d.focus].input.Update(key)
	return cmd
}

func (m *model) trackerVisibleRows() int {
	if m.dialog == nil {
		return 1
	}
	return detailBodyHeight(m.dialog, m.heightForDialog())
}

func (m *model) stepTrackerSelection(delta int) {
	d := m.dialog
	if d == nil || len(d.trackerEntries) == 0 {
		return
	}
	d.selected = (d.selected + delta) % len(d.trackerEntries)
	if d.selected < 0 {
		d.selected += len(d.trackerEntries)
	}
	m.ensureTrackerSelectionVisible()
}

func (m *model) ensureTrackerSelectionVisible() {
	d := m.dialog
	if d == nil || len(d.trackerEntries) == 0 {
		return
	}
	visible := m.trackerVisibleRows()
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+visible {
		d.scroll = d.selected - visible + 1
	}
	maxScroll := max(0, len(d.trackerEntries)-visible)
	d.scroll = min(max(0, d.scroll), maxScroll)
}

func (m *model) openTrackerAddDialog() {
	d := m.dialog
	if d == nil || d.torrentDetails == nil || strings.TrimSpace(d.torrentDetails.hash) == "" {
		return
	}
	d.kind = dialogTrackerAdd
	d.trackerTitle, d.title = d.title, "Add tracker"
	d.status = ""
	d.fields = []dialogField{newDialogField("URL", "", "https://tracker.example/announce", "Enter one tracker URL")}
	d.request = actionRequest{kind: "tracker-add", hashes: d.torrentDetails.hash}
	d.focusField(0)
}

func (m *model) openTrackerEditDialog() {
	d := m.dialog
	if d == nil || d.torrentDetails == nil || d.selected < 0 || d.selected >= len(d.trackerEntries) {
		return
	}
	entry := d.trackerEntries[d.selected]
	if !trackerURLIsEditable(entry.URL) {
		d.status = "Built-in discovery entries cannot be edited"
		return
	}
	// Entering edit mode is an intentional full-URL reveal: the exact URL is
	// needed for qBittorrent's editTracker match and is visible only after the
	// user asks to edit this row.
	d.kind = dialogTrackerEdit
	d.trackerTitle, d.title = d.title, "Edit tracker"
	d.status = ""
	d.fields = []dialogField{newDialogField("URL", entry.URL, "https://tracker.example/announce", "Replace with the new announce URL")}
	d.request = actionRequest{kind: "tracker-edit", hashes: d.torrentDetails.hash, original: entry.URL}
	d.focusField(0)
}

func (m *model) removeSelectedTracker() tea.Cmd {
	d := m.dialog
	if d == nil || d.torrentDetails == nil || d.selected < 0 || d.selected >= len(d.trackerEntries) {
		return nil
	}
	if m.busy != "" {
		d.status = "Finish the current request first"
		return nil
	}
	entry := d.trackerEntries[d.selected]
	if !trackerURLIsEditable(entry.URL) {
		d.status = "Built-in discovery entries cannot be removed"
		return nil
	}
	d.status = "Removing tracker…"
	return m.launchAction("tracker-remove", actionRequest{
		kind:   "tracker-remove",
		hashes: d.torrentDetails.hash,
		value:  entry.URL,
	})
}

func (m *model) submitTrackerDialog() tea.Cmd {
	d := m.dialog
	if d == nil || m.busy != "" {
		if d != nil && m.busy != "" {
			d.status = "Finish the current request first"
		}
		return nil
	}
	value := strings.TrimSpace(d.value(0))
	if value == "" {
		d.status = "Tracker URL is required"
		return nil
	}
	request := d.request
	request.value = value
	d.status = "Saving tracker…"
	return m.launchAction(request.kind, request)
}

func trackerURLIsEditable(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	return !strings.HasPrefix(value, "** [")
}

func trackerTabLines(d *dialog, width, visible int) []string {
	if len(d.trackerEntries) == 0 {
		return []string{viewMutedStyle.Render("No trackers available")}
	}
	start := min(max(0, d.scroll), max(0, len(d.trackerEntries)-visible))
	end := min(len(d.trackerEntries), start+visible)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		entry := d.trackerEntries[i]
		location := client.TrackerHost(entry.URL)
		if d.trackerReveal {
			location = entry.URL
		}
		line := fmt.Sprintf("%s  ·  %s", viewSafe(location), viewSafe(client.TrackerStatusLabel(entry.Status)))
		if i == d.selected {
			line = viewSelectedSty.Width(width).Render(viewFit("› "+line, width))
		} else {
			line = viewFit("  "+line, width)
		}
		lines = append(lines, line)
	}
	return lines
}
