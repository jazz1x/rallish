// Package ui is the rallish CLI presentation SSOT.
//
// All user-facing CLI output should go through this package so that
// colors, glyphs, and layout stay consistent across commands.
//
// Glyphs follow the PyClack convention used by sister projects
// (oh-my-borory / Vercel SDK):
//
//	◇  info / step
//	✓  success
//	■  error
//	⚠  warning
//	◆  prompt
//	└  footer / flow end
//
// Color is disabled automatically when stdout is not a TTY, when
// the NO_COLOR environment variable is set, or when the user passes
// --no-color via [DisableColor].
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Glyph is the SSOT for status glyphs.
type Glyph string

// Glyph values used by the Theme renderer. Add new glyphs here so
// every CLI surface picks the same character.
const (
	GlyphInfo   Glyph = "◇"
	GlyphOK     Glyph = "✓"
	GlyphErr    Glyph = "■"
	GlyphWarn   Glyph = "⚠"
	GlyphPrompt Glyph = "◆"
	GlyphFooter Glyph = "└"
	GlyphBullet Glyph = "•"
	GlyphArrow  Glyph = "→"
	GlyphPipe   Glyph = "│"
)

// ANSI color escape codes. Kept here so no other file in the
// codebase ever hand-rolls one (enforced by lefthook sweep).
const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiDim       = "\x1b[2m"
	ansiFgBlack   = "\x1b[30m"
	ansiFgRed     = "\x1b[31m"
	ansiFgGreen   = "\x1b[32m"
	ansiFgYellow  = "\x1b[33m"
	ansiFgBlue    = "\x1b[34m"
	ansiFgMagenta = "\x1b[35m"
	ansiFgCyan    = "\x1b[36m"
	ansiFgWhite   = "\x1b[37m"
	ansiFgGray    = "\x1b[90m"
	ansiBrGreen   = "\x1b[92m"
	ansiBrYellow  = "\x1b[93m"
	ansiBrRed     = "\x1b[91m"
	ansiBrBlue    = "\x1b[94m"
	ansiBrCyan    = "\x1b[96m"
)

// Style is a semantic color token. Map to ANSI in styleANSI().
type Style int

// Semantic style tokens. Add new tokens here, then teach styleANSI
// how to map them.
const (
	StylePlain Style = iota
	StyleInfo
	StyleSuccess
	StyleWarn
	StyleError
	StyleDim
	StylePrompt
	StyleAccent
	StyleHeading
)

// Theme controls how ui.* helpers render. The zero value is safe and
// auto-detects color support against os.Stdout. Build a Theme with
// [New] (recommended) or assign directly.
type Theme struct {
	// Out is the writer for normal output. Defaults to os.Stdout.
	Out io.Writer
	// ErrOut is the writer for errors and warnings. Defaults to os.Stderr.
	// (Named ErrOut to avoid colliding with the Err method.)
	ErrOut io.Writer
	// Color is true when ANSI escapes should be emitted. When false,
	// rendering falls back to plain ASCII.
	Color bool
	// In is the reader for prompts (kept here so tests can inject).
	// Defaults to os.Stdin.
	In io.Reader

	// reader is a cached bufio-backed line reader wrapping In, used by
	// Ask / Confirm / SelectOne. Cached so multiple prompts in one
	// wizard don't lose buffered bytes between calls.
	reader *bufioLineReader
}

// New returns a Theme bound to os.Stdin/Stdout/Stderr with color
// support auto-detected.
func New() *Theme {
	return &Theme{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
		In:     os.Stdin,
		Color:  detectColor(os.Stdout),
	}
}

// detectColor returns true if ANSI escapes are appropriate for w.
// Honors NO_COLOR (https://no-color.org) and falls back to a TTY check
// when w is an *os.File.
func detectColor(w io.Writer) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func (t *Theme) out() io.Writer {
	if t == nil || t.Out == nil {
		return os.Stdout
	}
	return t.Out
}

func (t *Theme) errw() io.Writer {
	if t == nil || t.ErrOut == nil {
		return os.Stderr
	}
	return t.ErrOut
}

func (t *Theme) styleANSI(s Style) (open, closeC string) {
	if t == nil || !t.Color {
		return "", ""
	}
	switch s {
	case StyleInfo:
		return ansiBrBlue, ansiReset
	case StyleSuccess:
		return ansiBrGreen, ansiReset
	case StyleWarn:
		return ansiBrYellow, ansiReset
	case StyleError:
		return ansiBrRed, ansiReset
	case StyleDim:
		return ansiDim, ansiReset
	case StylePrompt:
		return ansiBrCyan, ansiReset
	case StyleAccent:
		return ansiFgMagenta, ansiReset
	case StyleHeading:
		return ansiBold, ansiReset
	default:
		return "", ""
	}
}

// Sprint returns text wrapped with the style's ANSI codes (or the bare
// text when color is disabled).
func (t *Theme) Sprint(s Style, text string) string {
	open, closeC := t.styleANSI(s)
	if open == "" {
		return text
	}
	return open + text + closeC
}

// Info prints a [◇] info line on Out.
func (t *Theme) Info(format string, args ...any) {
	t.line(t.out(), GlyphInfo, StyleInfo, format, args...)
}

// Step prints a [◇  [n/N] text] progress line on Out.
func (t *Theme) Step(idx, total int, format string, args ...any) {
	prefix := fmt.Sprintf("[%d/%d] ", idx, total)
	t.line(t.out(), GlyphInfo, StyleInfo, prefix+format, args...)
}

// OK prints a [✓] success line on Out.
func (t *Theme) OK(format string, args ...any) {
	t.line(t.out(), GlyphOK, StyleSuccess, format, args...)
}

// StepOK prints a [✓  [n/N] text] success line on Out.
func (t *Theme) StepOK(idx, total int, format string, args ...any) {
	prefix := fmt.Sprintf("[%d/%d] ", idx, total)
	t.line(t.out(), GlyphOK, StyleSuccess, prefix+format, args...)
}

// Warn prints a [⚠] warning line on Err.
func (t *Theme) Warn(format string, args ...any) {
	t.line(t.errw(), GlyphWarn, StyleWarn, format, args...)
}

// Err prints a [■] error line on Err.
func (t *Theme) Err(format string, args ...any) {
	t.line(t.errw(), GlyphErr, StyleError, format, args...)
}

// StepErr prints a [■  [n/N] text] error line on Err.
func (t *Theme) StepErr(idx, total int, format string, args ...any) {
	prefix := fmt.Sprintf("[%d/%d] ", idx, total)
	t.line(t.errw(), GlyphErr, StyleError, prefix+format, args...)
}

// Detail prints a "│  text" indented detail line under the previous
// status line. Always dim (or plain when color is off).
func (t *Theme) Detail(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	open, closeC := t.styleANSI(StyleDim)
	_, _ = fmt.Fprintf(t.out(), "%s  %s%s%s\n", GlyphPipe, open, text, closeC)
}

// Heading prints a bold/plain heading with no glyph and a leading newline.
func (t *Theme) Heading(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	open, closeC := t.styleANSI(StyleHeading)
	_, _ = fmt.Fprintf(t.out(), "\n%s%s%s\n", open, text, closeC)
}

// Footer prints the closing "└  text" line followed by a trailing newline.
func (t *Theme) Footer(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	open, closeC := t.styleANSI(StyleAccent)
	_, _ = fmt.Fprintf(t.out(), "\n%s  %s%s%s\n\n", GlyphFooter, open, text, closeC)
}

// Bullet prints a "  • text" bullet point on Out.
func (t *Theme) Bullet(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(t.out(), "  %s %s\n", GlyphBullet, text)
}

// Kv prints a "  ✔ label  → value" wizard step in npx style.
func (t *Theme) Kv(label, value string) {
	t.KvSource(label, value, "")
}

// KvSource is like [Kv] with an optional dim source suffix in parens.
func (t *Theme) KvSource(label, value, source string) {
	openOK, closeOK := t.styleANSI(StyleSuccess)
	openDim, closeDim := t.styleANSI(StyleDim)
	suffix := ""
	if source != "" {
		suffix = fmt.Sprintf("  %s(%s)%s", openDim, source, closeDim)
	}
	_, _ = fmt.Fprintf(t.out(), "  %s%s%s %-18s %s %s%s\n",
		openOK, "✔", closeOK,
		label,
		GlyphArrow,
		value,
		suffix,
	)
}

// line is the common renderer for glyph-prefixed status lines.
func (t *Theme) line(w io.Writer, g Glyph, s Style, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	open, closeC := t.styleANSI(s)
	_, _ = fmt.Fprintf(w, "%s%s%s  %s\n", open, g, closeC, text)
}

// stripANSI removes ANSI escape sequences from s. Useful for tests.
func stripANSI(s string) string {
	var b strings.Builder
	in := []rune(s)
	for i := 0; i < len(in); i++ {
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '[' {
			j := i + 2
			for j < len(in) && in[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteRune(in[i])
	}
	return b.String()
}

// StripANSI is the public form of stripANSI (test/inspection use).
func StripANSI(s string) string { return stripANSI(s) }

// inputReader returns a single bufio-backed reader for prompt helpers.
// It is allocated lazily so each Theme keeps any read-ahead buffer
// across multiple Ask/Confirm/SelectOne calls — otherwise consecutive
// prompts in the same wizard would lose lines that bufio buffered.
func (t *Theme) inputReader() *bufioLineReader {
	if t.reader == nil {
		src := t.In
		if src == nil {
			src = os.Stdin
		}
		t.reader = newBufioLineReader(src)
	}
	return t.reader
}
