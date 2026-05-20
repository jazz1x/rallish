package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTestTheme(color bool) (*Theme, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	return &Theme{Out: out, ErrOut: err, Color: color}, out, err
}

func TestInfoOK_PlainNoColor(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.Info("hello")
	th.OK("done")
	got := out.String()
	if !strings.Contains(got, "◇  hello") {
		t.Errorf("missing info glyph line: %q", got)
	}
	if !strings.Contains(got, "✓  done") {
		t.Errorf("missing ok glyph line: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("plain mode should not emit ANSI: %q", got)
	}
}

func TestErrWarn_GoToStderr(t *testing.T) {
	th, out, errw := newTestTheme(false)
	th.Warn("careful")
	th.Err("boom")
	if out.Len() != 0 {
		t.Errorf("Warn/Err should not write to Out, got: %q", out.String())
	}
	got := errw.String()
	if !strings.Contains(got, "⚠  careful") {
		t.Errorf("missing warn glyph: %q", got)
	}
	if !strings.Contains(got, "■  boom") {
		t.Errorf("missing err glyph: %q", got)
	}
}

func TestStepNumbering(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.Step(2, 7, "do thing")
	th.StepOK(2, 7, "did thing")
	got := out.String()
	if !strings.Contains(got, "◇  [2/7] do thing") {
		t.Errorf("step format wrong: %q", got)
	}
	if !strings.Contains(got, "✓  [2/7] did thing") {
		t.Errorf("stepOK format wrong: %q", got)
	}
}

func TestKv(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.Kv("workdir", "/tmp")
	th.KvSource("preset", "solo-ralph", "embedded")
	got := out.String()
	if !strings.Contains(got, "workdir") || !strings.Contains(got, "/tmp") {
		t.Errorf("Kv missing fields: %q", got)
	}
	if !strings.Contains(got, "(embedded)") {
		t.Errorf("KvSource missing source: %q", got)
	}
}

func TestVisualWidth_CJKCountsAsTwo(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"랠리", 4},                // 2 hangul × 2
		{"테스트", 6},               // 3 hangul × 2
		{"テスト", 6},               // 3 katakana × 2
		{"漢字", 4},                // 2 han × 2
		{"hi랠리", 6},              // 2 ascii + 2 hangul × 2
		{"\x1b[32mok\x1b[0m", 2}, // ANSI stripped
	}
	for _, c := range cases {
		if got := visualWidth(c.in); got != c.want {
			t.Errorf("visualWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTable_CJKHeaderAligns(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.Render(Table{
		Headers: []string{"키", "값"},
		Rows: [][]string{
			{"preset", "solo-ralph"},
			{"랠리", "ok"},
		},
	})
	// Keep trailing whitespace — alignment relies on it. Drop the
	// final empty line that Fprintln leaves after the last row.
	raw := strings.TrimSuffix(out.String(), "\n")
	lines := strings.Split(raw, "\n")
	w := visualWidth(lines[0])
	for i, l := range lines {
		if got := visualWidth(l); got != w {
			t.Errorf("line %d width %d != header width %d (%q)", i, got, w, l)
		}
	}
}

func TestTableAligns(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.Render(Table{
		Headers: []string{"TYPE", "NAME", "DESC"},
		Rows: [][]string{
			{"preset", "solo-ralph", "single agent loop"},
			{"adapter", "claude", "default"},
		},
	})
	got := out.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), got)
	}
	// Column widths line up: "preset " + "  " + "solo-ralph" aligns with header.
	for _, l := range lines {
		if strings.HasPrefix(l, "preset ") {
			if !strings.Contains(l, "solo-ralph") {
				t.Errorf("expected preset row to contain name: %q", l)
			}
		}
	}
}

func TestColorEnabledEmitsANSI(t *testing.T) {
	th, out, _ := newTestTheme(true)
	th.OK("done")
	if !strings.Contains(out.String(), "\x1b[") {
		t.Errorf("color mode must emit ANSI: %q", out.String())
	}
	if StripANSI(out.String()) != "✓  done\n" {
		t.Errorf("StripANSI should leave glyph + text: %q", StripANSI(out.String()))
	}
}

func TestConfirmDefaultYesEnter(t *testing.T) {
	th, out, _ := newTestTheme(false)
	th.In = strings.NewReader("\n")
	ok, err := th.Confirm("proceed?", true)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("default-yes should return true on bare enter")
	}
	if !strings.Contains(out.String(), "Y/n") {
		t.Errorf("hint should show Y/n: %q", out.String())
	}
}

func TestAskReturnsDefault(t *testing.T) {
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("\n")
	got, err := th.Ask("name?", "anonymous")
	if err != nil {
		t.Fatal(err)
	}
	if got != "anonymous" {
		t.Errorf("expected default, got %q", got)
	}
}

func TestAskRequiredButEmpty_ReturnsErrPromptRequired(t *testing.T) {
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("\n")
	_, err := th.Ask("name?", "")
	if !errors.Is(err, ErrPromptRequired) {
		t.Errorf("want ErrPromptRequired, got %v", err)
	}
}

func TestAskEOFWithDefault_UsesDefault(t *testing.T) {
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("") // immediate EOF
	got, err := th.Ask("name?", "anon")
	if err != nil {
		t.Fatal(err)
	}
	if got != "anon" {
		t.Errorf("EOF with default should yield default, got %q", got)
	}
}

func TestSelectOneNumeric(t *testing.T) {
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("2\n")
	picked, idx, err := th.SelectOne("pick", []string{"a", "b", "c"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if picked != "b" || idx != 1 {
		t.Errorf("expected b/1, got %s/%d", picked, idx)
	}
}

func TestReaderSharedAcrossPrompts(t *testing.T) {
	// Regression: a Theme reused for multiple prompts in a wizard must
	// keep its bufio buffer alive across calls. Two Ask calls + one
	// Confirm call back-to-back must each see their own line.
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("first\nsecond\ny\n")
	a, err := th.Ask("q1?", "")
	if err != nil {
		t.Fatal(err)
	}
	if a != "first" {
		t.Errorf("first Ask: want first, got %q", a)
	}
	b, err := th.Ask("q2?", "")
	if err != nil {
		t.Fatal(err)
	}
	if b != "second" {
		t.Errorf("second Ask: want second, got %q", b)
	}
	ok, err := th.Confirm("q3?", false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("Confirm should have read 'y'")
	}
}

func TestSelectOneEnterUsesDefault(t *testing.T) {
	th, _, _ := newTestTheme(false)
	th.In = strings.NewReader("\n")
	picked, _, err := th.SelectOne("pick", []string{"a", "b"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if picked != "b" {
		t.Errorf("expected default b, got %s", picked)
	}
}
