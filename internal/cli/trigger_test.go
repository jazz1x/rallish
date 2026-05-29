package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTriggerDryRun(t *testing.T) {
	cmd := TriggerCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--dry-run", "자율 사이클"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "matched skill: autonomous-cycle") {
		t.Errorf("expected skill match line, got:\n%s", got)
	}
	if !strings.Contains(got, "dry-run: would execute:") {
		t.Errorf("expected dry-run header, got:\n%s", got)
	}
	if !strings.Contains(got, "rallish cycle start") {
		t.Errorf("expected cycle start command, got:\n%s", got)
	}
	if !strings.Contains(got, "--goal") {
		t.Errorf("expected --goal flag, got:\n%s", got)
	}
	if !strings.Contains(got, "--max-cycles") {
		t.Errorf("expected --max-cycles flag, got:\n%s", got)
	}
	if !strings.Contains(got, "--log-file") {
		t.Errorf("expected --log-file flag, got:\n%s", got)
	}
}

func TestTriggerUnknownPhrase(t *testing.T) {
	cmd := TriggerCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"this phrase matches nothing"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown phrase")
	}
	if !strings.Contains(err.Error(), "no skill matched trigger phrase") {
		t.Errorf("expected 'no skill matched trigger phrase' error, got: %v", err)
	}
}

func TestTriggerDefaultFlags(t *testing.T) {
	cmd := TriggerCmd()
	// Verify defaults without executing.
	f := cmd.Flags()
	mc, _ := f.GetInt("max-cycles")
	if mc != 10 {
		t.Errorf("max-cycles default = %d, want 10", mc)
	}
	md, _ := f.GetInt("max-duration")
	if md != 240 {
		t.Errorf("max-duration default = %d, want 240", md)
	}
	ag, _ := f.GetBool("auto-goal")
	if !ag {
		t.Error("auto-goal default = false, want true")
	}
}
