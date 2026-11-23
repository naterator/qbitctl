//go:build !plan9

package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const adaptiveRefreshPreference = "adaptive"

func loadRefreshInterval(path string) (time.Duration, error) {
	if strings.TrimSpace(path) == "" {
		return 0, nil
	}
	data, err := readPreferences(path, "refresh")
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	var disk struct {
		Interval string `json:"interval"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return 0, err
	}
	value := strings.TrimSpace(strings.ToLower(disk.Interval))
	if value == "" || value == adaptiveRefreshPreference || value == "default" {
		return 0, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		if err == nil {
			err = fmt.Errorf("refresh interval must be positive")
		}
		return 0, err
	}
	return interval, nil
}

func saveRefreshInterval(path string, interval time.Duration) error {
	value := adaptiveRefreshPreference
	if interval > 0 {
		value = interval.String()
	}
	return writePreferences(path, "refresh", map[string]string{"interval": value})
}

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
	if err := saveRefreshInterval(m.preferencesPath, interval); err != nil {
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
		return adaptiveRefreshPreference
	}
	return m.refreshIntervalSetting.String()
}

func parseRefreshInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == adaptiveRefreshPreference || value == "default" || value == "0" {
		return 0, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		return 0, fmt.Errorf("refresh interval must be adaptive or a positive duration such as 5s")
	}
	if interval < time.Second {
		return 0, fmt.Errorf("refresh interval must be at least 1s")
	}
	return interval, nil
}
