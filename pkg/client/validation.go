package client

import (
	"fmt"
	"strconv"
)

// ValidateTorrentTemplate parses tmpl with the same functions used for
// rendering it.
func ValidateTorrentTemplate(tmpl string) error {
	_, err := parseTorrentTemplate("validate", tmpl)
	return err
}

// IsGetField reports whether field names a property supported by get.
func IsGetField(field string) bool {
	field = CanonicalGetField(field)
	if field == "tracker-list" {
		return true
	}
	for _, name := range GetFieldNames {
		if field == name {
			return true
		}
	}
	return false
}

// IsSetField reports whether field names a property supported by set.
func IsSetField(field string) bool {
	field = CanonicalSetField(field)
	for _, name := range SetFieldNames {
		if field == name {
			return true
		}
	}
	return false
}

// ValidateSetValue checks a set value using the same parsers as the setters.
func ValidateSetValue(field, value string) error {
	field = CanonicalSetField(field)
	switch field {
	case "category", "tags":
		return nil
	case "up-limit", "dl-limit":
		_, err := parseLimit(value)
		return err
	case "ratio-limit":
		_, err := parseRatioLimit(value)
		return err
	case "seedtime-limit":
		seconds, err := parseSeedtimeLimit(value)
		if err != nil {
			return err
		}
		_, err = seedtimeLimitSecondsToMinutes(seconds)
		return err
	case "inactive-seedtime-limit":
		seconds, err := parseSeedtimeLimit(value)
		if err != nil {
			return err
		}
		_, err = seedtimeLimitSecondsToMinutes(seconds)
		return err
	case "share-limits-mode":
		_, err := parseShareLimitsMode(value)
		return err
	case "share-limit-action":
		_, err := parseShareLimitAction(value)
		return err
	case "seqdl", "autotmm", "superseed":
		_, err := parseToggleValue(value)
		return err
	default:
		return fmt.Errorf("unknown set field: %s", field)
	}
}

func parseRatioLimit(value string) (float64, error) {
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || !(ratio >= 0) {
		return 0, fmt.Errorf("invalid ratio value: %s", value)
	}
	return ratio, nil
}
