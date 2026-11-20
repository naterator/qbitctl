//go:build !plan9

package tui

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var resultSizePattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]*)?|\.[0-9]+)[[:space:]]*([a-z]*)$`)

var resultSizeUnits = map[string]int64{
	"":   1,
	"b":  1,
	"kb": 1_000,
	"mb": 1_000_000,
	"gb": 1_000_000_000,
	"tb": 1_000_000_000_000,
	"pb": 1_000_000_000_000_000,

	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
	"pib": 1 << 50,
}

// parseResultSize converts the size entered in the TUI into bytes.  Decimal
// (1000-based) and binary (1024-based) units are supported.  The arithmetic
// uses big.Rat to handle large sizes precisely and rounds a converted unit
// such as 1.2 GiB to the nearest byte.
func parseResultSize(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	matches := resultSizePattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("invalid size %q (use bytes, MB, or GiB)", raw)
	}

	factor, ok := resultSizeUnits[strings.ToLower(matches[2])]
	if !ok {
		return 0, fmt.Errorf("unsupported size unit %q (use bytes, MB, or GiB)", matches[2])
	}

	number, ok := new(big.Rat).SetString(matches[1])
	if !ok {
		return 0, fmt.Errorf("invalid size %q", raw)
	}
	number.Mul(number, big.NewRat(factor, 1))
	if factor == 1 && !number.IsInt() {
		return 0, fmt.Errorf("size %q is not a whole number of bytes", raw)
	}
	bytes, remainder := new(big.Int).QuoRem(number.Num(), number.Denom(), new(big.Int))
	if remainder.Lsh(remainder, 1).Cmp(number.Denom()) >= 0 {
		bytes.Add(bytes, big.NewInt(1))
	}
	if !bytes.IsInt64() {
		return 0, fmt.Errorf("size %q is outside the supported range", raw)
	}
	return bytes.Int64(), nil
}
