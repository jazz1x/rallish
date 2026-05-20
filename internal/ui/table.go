package ui

import (
	"fmt"
	"strings"
	"unicode"
)

// Table is a tiny aligned-column printer for the CLI.
// Columns auto-size to the widest cell (including header).
type Table struct {
	// Headers is the dim-styled header row.
	Headers []string
	// Rows is the body. Each row must have len(Headers) cells; cells
	// beyond that index are silently dropped by Render.
	Rows [][]string
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

// visualWidth returns the terminal column count for s. ANSI escapes
// are treated as zero-width; East-Asian wide characters (Hangul, CJK
// ideographs, Hiragana, Katakana, fullwidth forms) are counted as 2
// cells to match how most terminals render them. ASCII and other
// narrow characters count as 1.
func visualWidth(s string) int {
	clean := stripANSI(s)
	w := 0
	for _, r := range clean {
		w += runeWidth(r)
	}
	return w
}

// runeWidth returns 2 for East-Asian wide / fullwidth runes and 1 for
// the rest. Zero-width control chars are folded to 1 (we don't expect
// them in CLI table cells; ANSI is already stripped upstream).
func runeWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case unicode.Is(unicode.Hangul, r),
		unicode.Is(unicode.Han, r),
		unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r):
		return 2
	case r >= 0xFF00 && r <= 0xFFEF: // fullwidth forms
		return 2
	case r >= 0x2E80 && r <= 0x303E: // CJK radicals + symbols
		return 2
	}
	return 1
}

func padRight(s string, w int) string {
	pad := w - visualWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
