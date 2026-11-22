package client

import (
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/naterator/qbitctl/internal/configfile"
)

func FuzzParseLimit(f *testing.F) {
	for _, seed := range []string{"", "0", "512k", "-1", "9223372036854775k", "not-a-limit"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got, err := parseLimit(input)
		if err != nil {
			return
		}
		if strings.HasSuffix(strings.ToLower(input), "k") {
			digits := strings.TrimSuffix(strings.ToLower(input), "k")
			base, parseErr := strconv.ParseInt(digits, 10, 64)
			if parseErr != nil || base > math.MaxInt64/1024 || base < math.MinInt64/1024 {
				t.Fatalf("accepted overflowing limit %q as %d", input, got)
			}
		}
		formatted, roundTripErr := parseLimit(strconv.FormatInt(got, 10))
		if roundTripErr != nil || formatted != got {
			t.Fatalf("accepted limit %q as %d but decimal round trip was %d, %v", input, got, formatted, roundTripErr)
		}
	})
}

func FuzzParseSeedtimeLimit(f *testing.F) {
	for _, seed := range []string{"0", "1m", "3d", "1d2h30m", "-1", "-2", "30s", "not-a-duration"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		seconds, err := parseSeedtimeLimit(input)
		if err != nil {
			return
		}
		minutes, err := seedtimeLimitSecondsToMinutes(seconds)
		if err != nil {
			return
		}
		formatted := FormatSeedtimeLimitInput(minutes)
		if formatted == "" {
			return
		}
		formattedSeconds, parseErr := parseSeedtimeLimit(formatted)
		if parseErr != nil {
			t.Fatalf("FormatSeedtimeLimitInput(%d) = %q, which does not parse: %v", minutes, formatted, parseErr)
		}
		formattedMinutes, convertErr := seedtimeLimitSecondsToMinutes(formattedSeconds)
		if convertErr != nil || formattedMinutes != minutes {
			t.Fatalf("seedtime round trip %q -> %d -> %q -> %d, %v", input, minutes, formatted, formattedMinutes, convertErr)
		}
	})
}

func FuzzSanitizeServerText(f *testing.F) {
	for _, seed := range []string{"plain text", "line\nwith\ttabs", "\x00\x1b[31mred", strings.Repeat("x", 300)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		sanitized := SanitizeServerText(input)
		for _, r := range sanitized {
			if unicode.IsControl(r) {
				t.Fatalf("SanitizeServerText retained control character U+%04X", r)
			}
		}
		httpErr := newHTTPError(nil, []byte(input))
		if len([]rune(httpErr.Message)) > maxHTTPErrorMessageRunes+1 {
			t.Fatalf("HTTP error message has %d runes", len([]rune(httpErr.Message)))
		}
		for _, r := range httpErr.Message {
			if unicode.IsControl(r) {
				t.Fatalf("HTTP error retained control character U+%04X", r)
			}
		}
	})
}

func FuzzSearchResults(f *testing.F) {
	for _, seed := range []string{"magnet:?xt=urn:btih:abc", "https://example.test/file.torrent", "ftp://example.test/file.torrent", "javascript:alert(1)", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		accepted := isSearchDownloadURL(raw)
		if !accepted {
			return
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" && parsed.Scheme != "magnet" {
			t.Fatalf("accepted malformed search URL %q: %v", raw, err)
		}
		switch parsed.Scheme {
		case "magnet":
			if parsed.Query().Get("xt") == "" {
				t.Fatalf("accepted magnet URL without xt: %q", raw)
			}
		case "http", "https", "ftp":
			if parsed.Hostname() == "" {
				t.Fatalf("accepted URL without host: %q", raw)
			}
		default:
			t.Fatalf("accepted unsupported search URL scheme %q", parsed.Scheme)
		}
	})
}

func FuzzDecodeConfigFile(f *testing.F) {
	for _, seed := range []string{`{}`, `{"url":"https://example.test","user":"admin"}`, `{"password":false}`, `null`, `[]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var object configfile.Object
		if err := json.Unmarshal([]byte(input), &object); err != nil {
			return
		}
		decoded, err := decodeConfigFile(object)
		if err == nil && decoded.URL != "" {
			if strings.TrimSpace(decoded.URL) == "" {
				t.Fatal("decoded URL contained only whitespace")
			}
		}
	})
}

func FuzzValidateConfigURL(f *testing.F) {
	for _, seed := range []string{"http://localhost:8080", "https://qbt.example.test/qbt", "ftp://bad.example", "https://user:pass@example.test", "https://example.test/?x=1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		normalized, err := ValidateConfigURL(input)
		if err != nil {
			return
		}
		parsed, parseErr := url.Parse(normalized)
		if parseErr != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			t.Fatalf("accepted invalid normalized URL %q: %v", normalized, parseErr)
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			t.Fatalf("accepted forbidden URL components in %q", normalized)
		}
		if strings.HasSuffix(normalized, "/") {
			t.Fatalf("normalization retained trailing slash: %q", normalized)
		}
	})
}
