//go:build !plan9

package tui

import "testing"

func TestParseResultSize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "bare bytes", raw: "42", want: 42},
		{name: "bare exact decimal", raw: "42.0", want: 42},
		{name: "bytes with unit", raw: "42 B", want: 42},
		{name: "decimal unit", raw: "1.5 mb", want: 1_500_000},
		{name: "binary unit", raw: "1.5 MiB", want: 1_572_864},
		{name: "rounded binary unit", raw: "1.2 GiB", want: 1_288_490_189},
		{name: "rounded decimal unit", raw: "0.0000001 MB", want: 0},
		{name: "leading fraction", raw: ".5 GiB", want: 536_870_912},
		{name: "trimmed input", raw: " 2  GiB ", want: 2 << 30},
		{name: "case insensitive", raw: "2 gIb", want: 2 << 30},
		{name: "maximum int64", raw: "9223372036854775807", want: 9_223_372_036_854_775_807},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResultSize(tt.raw)
			if err != nil {
				t.Fatalf("parseResultSize(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("parseResultSize(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseResultSizeRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		"-1",
		"+1",
		"1e3",
		"1 024",
		"1 EB",
		"1 XB",
		"8 EiB",
		"0.5 B",
		"0.0001",
		"9223372036854775808",
		"9223372036854775.808 KB",
	} {
		t.Run(raw, func(t *testing.T) {
			if got, err := parseResultSize(raw); err == nil {
				t.Fatalf("parseResultSize(%q) = %d, want error", raw, got)
			}
		})
	}
}

func TestParseResultSizeUnitBoundaries(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want int64
	}{
		{raw: "1 PB", want: 1_000_000_000_000_000},
		{raw: "1 PiB", want: 1 << 50},
		{raw: "9223372036854775.807 KB", want: 9_223_372_036_854_775_807},
	} {
		got, err := parseResultSize(tt.raw)
		if err != nil {
			t.Fatalf("parseResultSize(%q) returned error: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseResultSize(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}
