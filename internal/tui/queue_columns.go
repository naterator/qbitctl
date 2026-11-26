//go:build !plan9

package tui

import (
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// queueColumnPreset keeps the default queue legible on small terminals while
// making the richer torrent fields available as an explicit saved choice.
type queueColumnPreset string

const (
	queueColumnsCompact queueColumnPreset = configfile.QueueColumnsCompact
	queueColumnsWide    queueColumnPreset = configfile.QueueColumnsWide
)

var queueColumnChoices = []struct {
	preset queueColumnPreset
	label  string
	detail string
}{
	{queueColumnsCompact, "Compact queue columns", "name, progress, state, and transfer rates"},
	{queueColumnsWide, "Wide queue columns", "adds size, ratio, ETA, category, and seeds when space allows"},
}

func (preset queueColumnPreset) label() string {
	for _, choice := range queueColumnChoices {
		if choice.preset == preset {
			return choice.label
		}
	}
	return ""
}

func normalizeQueueColumnPreset(value string) queueColumnPreset {
	switch queueColumnPreset(strings.ToLower(strings.TrimSpace(value))) {
	case queueColumnsWide:
		return queueColumnsWide
	default:
		return queueColumnsCompact
	}
}

func (m *model) setQueueColumns(preset queueColumnPreset) {
	preset = normalizeQueueColumnPreset(string(preset))
	m.queueColumns = preset
	if m.preferencesPath == "" {
		return
	}
	if err := saveQueueColumns(m.preferencesPath, preset); err != nil {
		m.setNotice("Could not save queue columns. "+err.Error(), true)
		return
	}
	m.setNotice("Queue columns saved", false)
}

func (m *model) openQueueColumnsDialog() {
	items := make([]dialogItem, 0, len(queueColumnChoices))
	for _, choice := range queueColumnChoices {
		items = append(items, dialogItem{label: choice.label, action: string(choice.preset), description: choice.detail})
	}
	selected := 0
	for i, item := range items {
		if item.action == string(m.queueColumns) {
			selected = i
			break
		}
	}
	m.dialog = &dialog{kind: dialogQueueColumns, title: "Queue columns", items: items, selected: selected}
}
