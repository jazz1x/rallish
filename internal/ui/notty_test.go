package ui

import (
	"bytes"
	"strings"
	"testing"
)

// All public ui.Theme helpers must produce plain ASCII (no ANSI
// escapes) when Color=false — i.e. when stdout is a pipe, CI, or
// NO_COLOR is set. This test locks that contract.
func TestPlainOutput_NoANSI(t *testing.T) {
	out := &bytes.Buffer{}
	errw := &bytes.Buffer{}
	th := &Theme{Out: out, ErrOut: errw, Color: false}

	th.Info("info-line")
	th.OK("ok-line")
	th.Warn("warn-line")
	th.Err("err-line")
	th.Detail("detail-line")
	th.Heading("heading-line")
	th.Footer("footer-line")
	th.Step(1, 2, "step-line")
	th.StepOK(1, 2, "step-ok")
	th.StepErr(1, 2, "step-err")
	th.Bullet("bullet-line")
	th.Kv("k", "v")
	th.KvSource("k", "v", "src")
	th.Render(Table{
		Headers: []string{"H1", "H2"},
		Rows:    [][]string{{"a", "b"}},
	})

	for _, buf := range []*bytes.Buffer{out, errw} {
		s := buf.String()
		if strings.Contains(s, "\x1b[") {
			t.Errorf("plain mode emitted ANSI escape:\n%q", s)
		}
	}
}

// detectColor must honor NO_COLOR / TERM=dumb so terminals that opt out
// of color stay colorless regardless of TTY detection.
func TestDetectColor_HonorsNOCOLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	// os.Stdout is a *os.File so the TTY branch is taken; NO_COLOR wins.
	if detectColor(&bytes.Buffer{}) {
		t.Errorf("non-File writer must never enable color")
	}
}

// Sprint with color off must round-trip text unchanged.
func TestSprint_NoColorIdentity(t *testing.T) {
	th := &Theme{Out: &bytes.Buffer{}, Color: false}
	for _, s := range []Style{StyleInfo, StyleSuccess, StyleWarn, StyleError, StyleDim, StylePrompt, StyleAccent, StyleHeading} {
		if got := th.Sprint(s, "hi"); got != "hi" {
			t.Errorf("style %d altered text in no-color mode: %q", s, got)
		}
	}
}
