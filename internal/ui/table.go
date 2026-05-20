package ui

import (
	"fmt"
	"strings"
)

// Table is a tiny aligned-column printer for the CLI.
// Columns auto-size to the widest cell (including header).
type Table struct {
	Headers []string
	Rows    [][]string
}

// Render writes the table to the theme's Out with dim header and
// plain rows. Use Sprint for cell colors if needed before adding.
func (t *Theme) Render(table Table) {
	widths := make([]int, len(table.Headers))
	for i, h := range table.Headers {
		widths[i] = visualWidth(h)
	}
	for _, row := range table.Rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if w := visualWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	openH, closeH := t.styleANSI(StyleDim)
	var hb strings.Builder
	for i, h := range table.Headers {
		hb.WriteString(padRight(h, widths[i]))
		if i < len(table.Headers)-1 {
			hb.WriteString("  ")
		}
	}
	_, _ = fmt.Fprintf(t.out(), "%s%s%s\n", openH, hb.String(), closeH)

	for _, row := range table.Rows {
		var rb strings.Builder
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			rb.WriteString(padRight(cell, widths[i]))
			if i < len(row)-1 {
				rb.WriteString("  ")
			}
		}
		_, _ = fmt.Fprintln(t.out(), rb.String())
	}
}

// visualWidth returns the column count for s, treating ANSI escapes
// as zero-width. Multi-byte glyphs count as 1 (good enough for our
// short labels).
func visualWidth(s string) int {
	clean := stripANSI(s)
	return len([]rune(clean))
}

func padRight(s string, w int) string {
	pad := w - visualWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
