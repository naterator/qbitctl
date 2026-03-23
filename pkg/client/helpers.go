package client

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func formatProgress(progress float64) string {
	return fmt.Sprintf("%.6f", progress)
}

func formatRatio(value float64) string {
	return fmt.Sprintf("%.6f", value)
}

func parseLimit(input string) (int64, error) {
	if input == "" {
		return -1, nil
	}

	var b strings.Builder
	start := 0
	if input[0] == '-' {
		b.WriteByte('-')
		start = 1
	}

	for start < len(input) && unicode.IsDigit(rune(input[start])) {
		b.WriteByte(input[start])
		start++
	}

	digits := b.String()
	if digits == "" || digits == "-" {
		return 0, fmt.Errorf("invalid limit")
	}

	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, err
	}

	if start < len(input) {
		if start+1 != len(input) || strings.ToLower(input[start:]) != "k" {
			return 0, fmt.Errorf("invalid limit suffix")
		}
		value *= 1024
	}

	return value, nil
}

func parseToggleValue(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("value must be 'true' or 'false'")
	}
}

func parseSeedtimeLimit(value string) (int64, error) {
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return 0, fmt.Errorf("invalid numeric seedtime")
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func validateHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	_, err := hex.DecodeString(strings.ToLower(hash))
	return err == nil
}

func trackerListOutput(trackers []TrackerEntry) string {
	out := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		switch tracker.URL {
		case "", "** [DHT] **", "** [PeX] **", "** [LSD] **":
			continue
		}
		out = append(out, tracker.URL)
	}
	return strings.Join(out, ",")
}

func looksLikeOpaqueCiphertext(value string) bool {
	if len(value) <= 32 || len(value)%2 != 0 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func looksLikeTorrentSourceURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "magnet", "http", "https", "ftp":
		return true
	default:
		return false
	}
}
