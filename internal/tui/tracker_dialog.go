//go:build !plan9

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// trackerService is optional so the small compatibility services used by the
// rest of the TUI tests do not need to implement tracker fetching. The real
// HTTP adapter implements it and therefore gets the structured tracker
// endpoint, including status, instead of the legacy comma-separated field.
type trackerService interface {
	trackers(context.Context, string) ([]client.TrackerEntry, error)
}

func (m *model) openTrackerDialog(title, hash string, entries []client.TrackerEntry) {
	m.dialog = &dialog{
		kind:           dialogTrackers,
		title:          title,
		trackerHash:    hash,
		trackerTitle:   title,
		trackerEntries: append([]client.TrackerEntry(nil), entries...),
	}
}

func (m *model) restoreTrackerListDialog() {
	d := m.dialog
	if d == nil {
		return
	}
	d.kind = dialogTrackers
	d.fields = nil
	d.focus = 0
	d.request = actionRequest{}
	d.status = ""
	if d.trackerTitle != "" {
		d.title = d.trackerTitle
	}
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

func (m *model) updateTrackerDialog(name string, key tea.KeyPressMsg) tea.Cmd {
	d := m.dialog
	if d == nil {
		return nil
	}
	if d.kind == dialogTrackers {
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
			d.selected = 0
			d.scroll = 0
		case "end", "G", "shift+g":
			d.selected = max(0, len(d.trackerEntries)-1)
			m.ensureTrackerSelectionVisible()
		case "v", "V":
			// Full URLs are never shown accidentally: the user must explicitly
			// toggle this view, or enter edit mode for one selected tracker.
			d.trackerReveal = !d.trackerReveal
		case "a", "A":
			m.openTrackerAddDialog()
		case "e", "E":
			m.openTrackerEditDialog()
		case "d", "D", "x", "X":
			return m.removeSelectedTracker()
		}
		return nil
	}

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
	return max(1, m.dialog.bodyHeight(m.heightForDialog()))
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
	if d == nil || strings.TrimSpace(d.trackerHash) == "" {
		return
	}
	d.kind = dialogTrackerAdd
	d.title = "Add tracker"
	d.status = ""
	d.fields = []dialogField{newDialogField("URL", "", "https://tracker.example/announce", "Enter one tracker URL")}
	d.request = actionRequest{kind: "tracker-add", hashes: d.trackerHash}
	d.focusField(0)
}

func (m *model) openTrackerEditDialog() {
	d := m.dialog
	if d == nil || d.selected < 0 || d.selected >= len(d.trackerEntries) {
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
	d.title = "Edit tracker"
	d.status = ""
	d.fields = []dialogField{newDialogField("URL", entry.URL, "https://tracker.example/announce", "Replace with the new announce URL")}
	d.request = actionRequest{kind: "tracker-edit", hashes: d.trackerHash, original: entry.URL}
	d.focusField(0)
}

func (m *model) removeSelectedTracker() tea.Cmd {
	d := m.dialog
	if d == nil || d.selected < 0 || d.selected >= len(d.trackerEntries) {
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
		hashes: d.trackerHash,
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

func trackerDialogLines(d *dialog, width, visible int) []string {
	if d == nil || len(d.trackerEntries) == 0 {
		return []string{viewMutedStyle.Render("No trackers available")}
	}
	if visible < 1 {
		visible = 1
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
		line = viewFit(line, width)
		if i == d.selected {
			line = "› " + line
			line = viewSelectedSty.Width(width).Render(line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return lines
}
