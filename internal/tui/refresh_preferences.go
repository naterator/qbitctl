//go:build !plan9

package tui

import (
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
)

func (m *model) refreshIntervalLabel() string {
	if m.refreshIntervalSetting <= 0 {
		return "Adaptive (5s active / 15s idle)"
	}
	return m.refreshIntervalSetting.String()
}

func (m *model) setRefreshInterval(interval time.Duration) {
	if interval < 0 {
		interval = 0
	}
	m.refreshIntervalSetting = interval
	if m.preferencesPath == "" {
		m.setNotice("Refresh interval selected for this session", false)
		return
	}
	if err := configfile.SaveRefreshInterval(m.preferencesPath, interval); err != nil {
		m.setNotice("Could not save refresh interval. "+err.Error(), true)
		return
	}
	m.setNotice("Refresh interval saved", false)
}

func (m *model) openRefreshIntervalDialog() {
	m.dialog = &dialog{
		kind:   dialogRefreshInterval,
		title:  "Automatic refresh interval",
		fields: []dialogField{newDialogField("Interval", m.refreshIntervalInput(), "adaptive, 5s, or 30s", "Adaptive uses 5 seconds for active transfers and 15 seconds when idle")},
	}
	m.dialog.focusField(0)
}

func (m *model) refreshIntervalInput() string {
	if m.refreshIntervalSetting <= 0 {
		return configfile.AdaptiveRefresh
	}
	return m.refreshIntervalSetting.String()
}
