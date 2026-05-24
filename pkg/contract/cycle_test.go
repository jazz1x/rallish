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
