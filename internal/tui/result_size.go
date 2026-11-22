//go:build !plan9

package tui

import (
	"github.com/naterator/qbitctl/internal/searchresults"
)

// parseResultSize converts the size entered in the TUI into bytes. The shared
// parser also serves the CLI so both interfaces accept the same units.
func parseResultSize(raw string) (int64, error) {
	return searchresults.ParseSize(raw)
}
