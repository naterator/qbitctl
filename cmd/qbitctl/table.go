package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// writeTable aligns columns by their displayed width, including ANSI swatches.
func writeTable(out io.Writer, style cliStyle, headings []string, rows [][]string) error {
	widths := make([]int, len(headings))
	for i, heading := range headings {
		widths[i] = ansi.StringWidth(heading)
	}
	for _, row := range rows {
		if len(row) != len(headings) {
			return fmt.Errorf("table row has %d columns, expected %d", len(row), len(headings))
		}
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}

	writeRow := func(cells []string, header bool) error {
		var line strings.Builder
		for i, cell := range cells {
			text := cell
			if header {
				text = style.heading(cell)
			}
			line.WriteString(text)
			if i < len(cells)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(cell)+2))
			}
		}
		line.WriteByte('\n')
		_, err := io.WriteString(out, line.String())
		return err
	}
	if err := writeRow(headings, true); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(row, false); err != nil {
			return err
		}
	}
	return nil
}
