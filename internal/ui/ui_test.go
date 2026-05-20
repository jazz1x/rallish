package ui

import (
	"bytes"
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
