package configfile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const AdaptiveRefresh = "adaptive"

func ReadRefreshInterval(path string) (time.Duration, error) {
	if strings.TrimSpace(path) == "" {
		return 0, nil
	}
	data, err := ReadUIPreference(path, "refresh")
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
	if value == "" || value == AdaptiveRefresh || value == "default" {
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

func SaveRefreshInterval(path string, interval time.Duration) error {
	value := AdaptiveRefresh
	if interval > 0 {
		value = interval.String()
	}
	return WriteUISection(path, "refresh", map[string]string{"interval": value})
}

func ParseRefreshInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == AdaptiveRefresh || value == "default" || value == "0" {
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
