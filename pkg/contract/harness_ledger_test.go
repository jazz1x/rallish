package contract

import "testing"

func TestNewHarnessLedgerEntryCopiesFiles(t *testing.T) {
	files := []string{"a.go", "b.go"}
	entry := NewHarnessLedgerEntry(123, "cyc_1", LedgerEventGatePassed, "tests passed", files)

	if entry.At != 123 {
		t.Fatalf("at = %d, want 123", entry.At)
	}
	if entry.CycleID != "cyc_1" {
		t.Fatalf("cycle_id = %q, want cyc_1", entry.CycleID)
	}
	if entry.Type != LedgerEventGatePassed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGatePassed)
	}
	if entry.Summary != "tests passed" {
		t.Fatalf("summary = %q", entry.Summary)
	}
	if len(entry.Files) != 2 || entry.Files[0] != "a.go" || entry.Files[1] != "b.go" {
		t.Fatalf("files = %v", entry.Files)
	}

	files[0] = "mutated.go"
	if entry.Files[0] == "mutated.go" {
		t.Fatal("entry should copy files")
	}
}

func TestNewGateLedgerEntryPassed(t *testing.T) {
	report := GateReport{Gate: "audit", Passed: true, Stdout: "ok"}

	entry := NewGateLedgerEntry(456, "cyc_2", report)
	if entry.Type != LedgerEventGatePassed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGatePassed)
	}
	if entry.Gate != "audit" {
		t.Fatalf("gate = %q, want audit", entry.Gate)
	}
	if entry.Summary != "ok" {
		t.Fatalf("summary = %q, want ok", entry.Summary)
	}
}

func TestNewGateLedgerEntryFailed(t *testing.T) {
	report := GateReport{Gate: "audit", Passed: false, Stderr: "failed"}

	entry := NewGateLedgerEntry(789, "cyc_3", report)
	if entry.Type != LedgerEventGateFailed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGateFailed)
	}
	if entry.Gate != "audit" {
		t.Fatalf("gate = %q, want audit", entry.Gate)
	}
	if entry.Summary != "failed" {
		t.Fatalf("summary = %q, want failed", entry.Summary)
	}
}

func TestNewAgentTurnLedgerEntry(t *testing.T) {
	artifacts := []string{"agent.md"}
	resp := TurnResponse{
		Summary:   "implemented turn",
		Artifacts: artifacts,
	}

	entry := NewAgentTurnLedgerEntry(876, "cyc_5", "builder", resp)
	if entry.Type != LedgerEventAgentTurn {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventAgentTurn)
	}
	if entry.Agent != "builder" {
		t.Fatalf("agent = %q, want builder", entry.Agent)
	}
	if entry.Summary != "implemented turn" {
		t.Fatalf("summary = %q, want implemented turn", entry.Summary)
	}
	if len(entry.Files) != 1 || entry.Files[0] != "agent.md" {
		t.Fatalf("files = %v", entry.Files)
	}

	artifacts[0] = "mutated.md"
	if entry.Files[0] == "mutated.md" {
		t.Fatal("entry should copy artifacts")
	}
}

func TestNewHandoffLedgerEntry(t *testing.T) {
	artifacts := []string{"handoff.md"}
	resp := TurnResponse{
		HandoffTo: "reviewer",
		Summary:   "ready for review",
		Artifacts: artifacts,
	}

	entry := NewHandoffLedgerEntry(987, "cyc_4", "builder", resp)
	if entry.Type != LedgerEventHandoffCreated {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventHandoffCreated)
	}
	if entry.Agent != "builder" {
		t.Fatalf("agent = %q, want builder", entry.Agent)
	}
	if entry.HandoffTo != "reviewer" {
		t.Fatalf("handoff_to = %q, want reviewer", entry.HandoffTo)
	}
	if entry.Summary != "ready for review" {
		t.Fatalf("summary = %q, want ready for review", entry.Summary)
	}
	if len(entry.Files) != 1 || entry.Files[0] != "handoff.md" {
		t.Fatalf("files = %v", entry.Files)
	}

	artifacts[0] = "mutated.md"
	if entry.Files[0] == "mutated.md" {
		t.Fatal("entry should copy artifacts")
	}
}
