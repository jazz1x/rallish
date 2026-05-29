package contract

import (
	"fmt"
	"testing"
	"time"
)

func TestParseCyclePhase(t *testing.T) {
	tests := []struct {
		input string
		want  CyclePhase
		err   bool
	}{
		{"preflight", CyclePhasePreflight, false},
		{"audit", CyclePhaseAudit, false},
		{"philosophy", CyclePhasePhilosophy, false},
		{"polish", CyclePhasePolish, false},
		{"commit", CyclePhaseCommit, false},
		{"handoff", CyclePhaseHandoff, false},
		{"halted", CyclePhaseHalted, false},
		{"unknown", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseCyclePhase(tt.input)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseCyclePhase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCyclePhaseNext(t *testing.T) {
	tests := []struct {
		phase CyclePhase
		want  CyclePhase
	}{
		{CyclePhasePreflight, CyclePhaseAudit},
		{CyclePhaseAudit, CyclePhasePhilosophy},
		{CyclePhasePhilosophy, CyclePhasePolish},
		{CyclePhasePolish, CyclePhaseCommit},
		{CyclePhaseCommit, CyclePhaseHandoff},
		{CyclePhaseHandoff, CyclePhaseHalted},
		{CyclePhaseHalted, CyclePhaseHalted},
	}
	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			if got := tt.phase.Next(); got != tt.want {
				t.Fatalf("%q.Next() = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

func TestParseHaltReason(t *testing.T) {
	tests := []struct {
		input string
		want  HaltReason
		err   bool
	}{
		{"self-audit-violation", HaltSelfAuditViolation, false},
		{"ssh-auth-failed", HaltSSHAuthFailed, false},
		{"max-cycles-reached", HaltMaxCyclesReached, false},
		{"gate-failure", HaltGateFailure, false},
		{"user-requested", HaltUserRequested, false},
		{"preflight-failed", HaltPreflightFailed, false},
		{"stuck", HaltStuck, false},
		{"budget-exceeded", HaltBudgetExceeded, false},
		{"unknown", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseHaltReason(tt.input)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseHaltReason(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCycleStateCanAdvance(t *testing.T) {
	tests := []struct {
		name  string
		state *CycleState
		want  bool
	}{
		{"fresh", &CycleState{Halted: false, CompletedCycles: 0, MaxCycles: 5}, true},
		{"at limit", &CycleState{Halted: false, CompletedCycles: 5, MaxCycles: 5}, false},
		{"over limit", &CycleState{Halted: false, CompletedCycles: 6, MaxCycles: 5}, false},
		{"halted", &CycleState{Halted: true, CompletedCycles: 0, MaxCycles: 5}, false},
		{"no limit", &CycleState{Halted: false, CompletedCycles: 100, MaxCycles: 0}, true},
		{"duration exceeded", &CycleState{Halted: false, CompletedCycles: 0, MaxCycles: 0, MaxDurationMinutes: 1, StartedAt: time.Now().Add(-2 * time.Minute).UnixMilli()}, false},
		{"duration not exceeded", &CycleState{Halted: false, CompletedCycles: 0, MaxCycles: 0, MaxDurationMinutes: 60, StartedAt: time.Now().UnixMilli()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.CanAdvance(); got != tt.want {
				t.Fatalf("CanAdvance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHaltReasonSuccess(t *testing.T) {
	r, err := ParseHaltReason("success")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != HaltSuccess {
		t.Fatalf("got %q, want success", r)
	}
}

func TestCycleStateCanAdvanceEdgeCases(t *testing.T) {
	// StartedAt=0 with MaxDurationMinutes>0 → duration check is skipped.
	s := &CycleState{Halted: false, CompletedCycles: 0, MaxCycles: 0, MaxDurationMinutes: 1, StartedAt: 0}
	if !s.CanAdvance() {
		t.Fatal("CanAdvance() = false when StartedAt=0, want true (duration check skipped)")
	}

	// Halted with HaltSuccess.
	s2 := &CycleState{Halted: true, HaltReason: HaltSuccess, CompletedCycles: 5, MaxCycles: 10}
	if s2.CanAdvance() {
		t.Fatal("CanAdvance() = true when halted, want false")
	}
}

func TestNewCycleEvent(t *testing.T) {
	state := &CycleState{
		ID:              "cyc_123",
		Phase:           CyclePhaseAudit,
		CompletedCycles: 2,
		Halted:          false,
		HaltReason:      "",
		LastFailedGate:  "lint",
	}
	evt := NewCycleEvent(state)
	if evt.CycleID != state.ID {
		t.Fatalf("CycleID = %q, want %q", evt.CycleID, state.ID)
	}
	if evt.Phase != state.Phase {
		t.Fatalf("Phase = %q, want %q", evt.Phase, state.Phase)
	}
	if evt.CompletedCycles != state.CompletedCycles {
		t.Fatalf("CompletedCycles = %d, want %d", evt.CompletedCycles, state.CompletedCycles)
	}
	if evt.Halted != state.Halted {
		t.Fatalf("Halted = %v, want %v", evt.Halted, state.Halted)
	}
	if evt.LastFailedGate != state.LastFailedGate {
		t.Fatalf("LastFailedGate = %q, want %q", evt.LastFailedGate, state.LastFailedGate)
	}
	if evt.At == 0 {
		t.Fatal("At timestamp is zero")
	}
}

func TestGateResultImplementations(t *testing.T) {
	gs := GateSuccess{R: GateReport{Gate: "lint", Passed: true}}
	if gs.Report().Gate != "lint" {
		t.Fatalf("GateSuccess.Report().Gate = %q, want lint", gs.Report().Gate)
	}

	gf := GateFailure{R: GateReport{Gate: "test", Passed: false}, Reason: HaltGateFailure}
	if gf.Report().Gate != "test" {
		t.Fatalf("GateFailure.Report().Gate = %q, want test", gf.Report().Gate)
	}
	if gf.Reason != HaltGateFailure {
		t.Fatalf("GateFailure.Reason = %q, want gate-failure", gf.Reason)
	}

	gw := GateWarning{R: GateReport{Gate: "philosophy", Passed: true}}
	if gw.Report().Gate != "philosophy" {
		t.Fatalf("GateWarning.Report().Gate = %q, want philosophy", gw.Report().Gate)
	}
}

func TestCycleStateShouldRotateAgent(t *testing.T) {
	tests := []struct {
		completed int
		want      bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, false},
		{5, false},
		{6, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.completed), func(t *testing.T) {
			s := &CycleState{CompletedCycles: tt.completed}
			if got := s.ShouldRotateAgent(); got != tt.want {
				t.Fatalf("ShouldRotateAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}
