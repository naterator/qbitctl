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
	if value == "-1" || value == "-2" {
		// qBittorrent reserves -1 for no limit and -2 for the global limit.
		return strconv.ParseInt(value, 10, 64)
	}

	if value == "" {
		return 0, fmt.Errorf("invalid numeric seedtime")
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return 0, fmt.Errorf("invalid numeric seedtime")
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

// seedtimeLimitSecondsToMinutes converts the CLI's documented seconds to the
// qBittorrent Web API's minute-based wire value. The API sentinels are already
// wire values and must pass through unchanged. Positive values must describe a
// whole number of minutes; rejecting a remainder avoids silently changing the
// requested limit.
func seedtimeLimitSecondsToMinutes(seconds int64) (int64, error) {
	switch seconds {
	case -1, -2:
		return seconds, nil
	case 0:
		return 0, nil
	}
	if seconds < 0 {
		return 0, fmt.Errorf("seedtime must be non-negative seconds, -1 (unlimited), or -2 (global limit)")
	}
	if seconds%60 != 0 {
		return 0, fmt.Errorf("seedtime must be a whole number of minutes (seconds must be divisible by 60)")
	}
	return seconds / 60, nil
}

// SeedtimeLimitInputValue formats an API seed-time limit for an editable CLI
// or TUI field. qBittorrent reports ordinary limits in minutes, while qbitctl's
// user-facing setter accepts seconds; the API sentinels remain unchanged.
func SeedtimeLimitInputValue(minutes int64) string {
	if minutes == -1 || minutes == -2 {
		return strconv.FormatInt(minutes, 10)
	}
	if minutes < 0 {
		return ""
	}
	// There is no representable seconds value for a minute count this large.
	// Leave the field blank rather than displaying minutes as seconds and then
	// silently changing the limit if the user submits it.
	const maxInt64 = int64(^uint64(0) >> 1)
	if minutes > maxInt64/60 {
		return ""
	}
	return strconv.FormatInt(minutes*60, 10)
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
