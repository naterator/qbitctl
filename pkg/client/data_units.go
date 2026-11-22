package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// DataUnits controls how byte counts and transfer rates are rendered.
type DataUnits string

const (
	// DataUnitsHuman uses binary units such as KiB and MiB.
	DataUnitsHuman DataUnits = "human"
	// DataUnitsBytes renders exact byte counts.
	DataUnitsBytes DataUnits = "bytes"
)

const dataUnitsConfigKey = "data_units"

// ParseDataUnits parses the persisted or command-line data-unit setting.
// An empty value means the human-readable default, which keeps older config
// files compatible with the current display behavior.
func ParseDataUnits(raw string) (DataUnits, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return DataUnitsHuman, nil
	case string(DataUnitsHuman):
		return DataUnitsHuman, nil
	case string(DataUnitsBytes):
		return DataUnitsBytes, nil
	default:
		return "", fmt.Errorf("invalid data units %q (use human or bytes)", raw)
	}
}

// FormatSize formats a byte count according to these units. The zero value
// and unknown values use human-readable binary units. Negative values
// represent unavailable metrics and render as ?.
func (units DataUnits) FormatSize(value int64) string {
	if units == DataUnitsBytes {
		return formatByteCount(value)
	}
	return formatHumanByteCount(value)
}

// FormatRate formats a transfer rate according to these units. Negative
// values represent unavailable metrics and render as ?.
func (units DataUnits) FormatRate(value int64) string {
	formatted := units.FormatSize(value)
	if formatted == "?" {
		return formatted
	}
	return formatted + "/s"
}

func formatByteCount(value int64) string {
	if value < 0 {
		return "?"
	}
	return fmt.Sprintf("%d B", value)
}

func formatHumanByteCount(value int64) string {
	if value < 0 {
		return "?"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}

// ReadDataUnits reads the optional root-level data_units setting. Missing,
// empty, and legacy config values use the human-readable default.
func ReadDataUnits(path string) (DataUnits, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("config path is empty")
	}
	object, err := configfile.Read(path)
	if err != nil {
		return "", err
	}
	value, ok := configValue(object, dataUnitsConfigKey)
	if !ok {
		return DataUnitsHuman, nil
	}
	var raw string
	if err := json.Unmarshal(value, &raw); err != nil {
		return "", fmt.Errorf("decode config field %q: %w", dataUnitsConfigKey, err)
	}
	units, err := ParseDataUnits(raw)
	if err != nil {
		return "", fmt.Errorf("decode config field %q: %w", dataUnitsConfigKey, err)
	}
	return units, nil
}

// SetDataUnits atomically updates the optional root-level data_units setting
// while preserving credentials and unrelated config fields.
func SetDataUnits(path string, units DataUnits) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	parsed, err := ParseDataUnits(string(units))
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("encode config field %q: %w", dataUnitsConfigKey, err)
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		setConfigValue(object, dataUnitsConfigKey, encoded)
		return true, nil
	})
}
