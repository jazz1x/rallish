package adapter

import (
	"strings"
	"unicode/utf8"
)

// Snippet returns a bounded, trailing-trimmed view of adapter output for error
// diagnostics — capped so a large stdout cannot bloat the error or the ledger.
// The tail is kept (it usually holds the actual error/notice), prefixed with an
// ellipsis when truncated, and always sliced to a valid UTF-8 rune boundary.
func Snippet(b []byte, maxLen int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= maxLen {
		return s
	}
	tail := s[len(s)-maxLen:]
	// A byte-offset cut can land inside a multibyte rune, leaving a dangling
	// continuation byte at the front; advance past it so the snippet is always
	// valid UTF-8 (drops at most the partial leading rune, keeping the byte cap).
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return "…" + tail
}
