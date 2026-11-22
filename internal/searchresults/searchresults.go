// Package searchresults contains the result filtering and ordering shared by
// the command-line search output and the terminal UI.
package searchresults

import (
	"cmp"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/pkg/client"
)

// Sort controls the order in which search results are displayed.
type Sort string

const (
	Original    Sort = "original"
	SeedersDesc Sort = "seeders-desc"
	SeedersAsc  Sort = "seeders-asc"
	SizeDesc    Sort = "size-desc"
	SizeAsc     Sort = "size-asc"
)

var sortChoices = []Sort{Original, SeedersDesc, SeedersAsc, SizeDesc, SizeAsc}

var sortChoiceNames = []string{string(Original), string(SeedersDesc), string(SeedersAsc), string(SizeDesc), string(SizeAsc)}

// ParseSort validates a search-result sort name.
func ParseSort(raw string) (Sort, error) {
	sort := Sort(strings.ToLower(strings.TrimSpace(raw)))
	for _, choice := range sortChoices {
		if sort == choice {
			return sort, nil
		}
	}
	return "", fmt.Errorf("invalid search result sort %q (use %s)", raw, strings.Join(sortChoiceNames, ", "))
}

// Compare returns the ordering between left and right for s. It returns a
// negative value when left comes first, zero when the values are equal, and a
// positive value when right comes first. Unknown metrics (-1) sort last.
func (s Sort) Compare(left, right client.SearchResult) int {
	switch s {
	case SeedersDesc, SeedersAsc:
		return compareMetric(int64(left.Seeders), int64(right.Seeders), s == SeedersDesc)
	case SizeDesc, SizeAsc:
		return compareMetric(left.Size, right.Size, s == SizeDesc)
	default:
		return 0
	}
}

func compareMetric(left, right int64, descending bool) int {
	if left < 0 {
		if right < 0 {
			return 0
		}
		return 1
	}
	if right < 0 {
		return -1
	}
	if descending {
		return cmp.Compare(right, left)
	}
	return cmp.Compare(left, right)
}

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

// ParseSize converts a human-entered size into bytes. Decimal (1000-based)
// and binary (1024-based) units are supported. Converted fractional units are
// rounded to the nearest byte.
func ParseSize(raw string) (int64, error) {
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

// FormatSize formats a byte count using the same binary units as the TUI.
func FormatSize(value int64) string {
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

// Filter selects search results by free-form text and numeric bounds.
type Filter struct {
	Query      string
	MinSeeders *int64
	MinSize    *int64
	MaxSize    *int64
}

// Validate checks bounds that must hold independently of any result.
func (f Filter) Validate() error {
	if f.MinSeeders != nil && *f.MinSeeders < 0 {
		return fmt.Errorf("minimum seeders must be nonnegative")
	}
	if f.MinSize != nil && *f.MinSize < 0 {
		return fmt.Errorf("minimum size must be nonnegative")
	}
	if f.MaxSize != nil && *f.MaxSize < 0 {
		return fmt.Errorf("maximum size must be nonnegative")
	}
	if f.MinSize != nil && f.MaxSize != nil && *f.MinSize > *f.MaxSize {
		return fmt.Errorf("minimum size cannot exceed maximum size")
	}
	return nil
}

// Matches reports whether result satisfies every active filter criterion.
func (f Filter) Matches(result client.SearchResult) bool {
	if f.MinSeeders != nil && int64(result.Seeders) < *f.MinSeeders {
		return false
	}
	if (f.MinSize != nil || f.MaxSize != nil) && result.Size < 0 {
		return false
	}
	if f.MinSize != nil && result.Size < *f.MinSize {
		return false
	}
	if f.MaxSize != nil && result.Size > *f.MaxSize {
		return false
	}

	terms := strings.Fields(strings.ToLower(f.Query))
	if len(terms) == 0 {
		return true
	}
	text := strings.ToLower(result.Name + " " + result.Source)
	if result.ID > 0 {
		text += " " + strconv.FormatInt(result.ID, 10)
	}
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// Apply filters, orders, and limits results. The input slice is never
// modified; relative order is preserved for equal sort keys.
func Apply(results []client.SearchResult, filter Filter, sort Sort, limit int) []client.SearchResult {
	filtered := make([]client.SearchResult, 0, len(results))
	for _, result := range results {
		if filter.Matches(result) {
			filtered = append(filtered, result)
		}
	}
	if sort != Original {
		slices.SortStableFunc(filtered, sort.Compare)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}
