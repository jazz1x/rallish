package adapter

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSnippet(t *testing.T) {
	// Short input is returned trimmed, verbatim.
	if got := Snippet([]byte("  hello  "), 2000); got != "hello" {
		t.Fatalf("Snippet short = %q, want %q", got, "hello")
	}
	// Empty stays empty (no spurious ellipsis on a parse failure with no output).
	if got := Snippet([]byte("   "), 2000); got != "" {
		t.Fatalf("Snippet empty = %q, want empty", got)
	}
	// Oversized input is capped to the TRAILING max bytes (the tail usually holds
	// the actual error/notice) with a leading ellipsis, so it never bloats an error.
	big := strings.Repeat("x", 5000)
	got := Snippet([]byte(big), 2000)
	// "…" is 3 bytes in UTF-8, so the cap yields 2000 tail bytes + the ellipsis.
	if len(got) != 2000+len("…") {
		t.Fatalf("Snippet cap len = %d, want %d (2000 tail + ellipsis)", len(got), 2000+len("…"))
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("oversized Snippet must start with an ellipsis")
	}
	if !strings.HasSuffix(got, "x") {
		t.Fatalf("oversized Snippet must keep the trailing bytes")
	}
	// A byte-offset cap landing inside a multibyte rune must not emit invalid
	// UTF-8: "한" is 3 bytes, so cap 4 cuts mid-rune. The dangling leading byte is
	// dropped, leaving a clean rune-boundary tail (no U+FFFD replacement char).
	multibyte := Snippet([]byte("한한한한"), 4)
	if !utf8.ValidString(multibyte) {
		t.Fatalf("multibyte Snippet = % x, must be valid UTF-8", []byte(multibyte))
	}
	if r, _ := utf8.DecodeRuneInString(strings.TrimPrefix(multibyte, "…")); r == utf8.RuneError {
		t.Fatalf("multibyte Snippet = %q, must not start with a replacement rune", multibyte)
	}
}
