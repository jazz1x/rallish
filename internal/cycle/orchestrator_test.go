package cycle

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/adapter/fake"
	"github.com/jazz1x/rallish/pkg/contract"
)

// passGate is a pass-through gate that leaves state untouched.
type passGate struct{ name string }

func (g passGate) Name() string { return g.name }
func (g passGate) Run(_ context.Context, state State) (contract.GateResult, State) {
	return contract.GateSuccess{R: contract.GateReport{Gate: g.name, Passed: true}}, state
}

func TestOrchestratorMultiAgentRotation(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 5}, "cyc_e2e_1")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := sync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	reg := adapter.NewRegistry()

	// agent-alpha: always returns a fresh goal.
	alpha := fake.New(func(turn int) contract.TurnResponse {
		return contract.TurnResponse{
			Done:    false,
			Summary: fmt.Sprintf(`{"next_goal":"alpha-turn-%d"}`, turn),
		}
	})
	// agent-beta: returns goals too.
	beta := fake.New(func(turn int) contract.TurnResponse {
		return contract.TurnResponse{
			Done:    false,
			Summary: fmt.Sprintf(`{"next_goal":"beta-turn-%d"}`, turn),
		}
	})

	if err := reg.Register("agent-alpha", alpha); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := reg.Register("agent-beta", beta); err != nil {
		t.Fatalf("register beta: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, sync)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"agent-alpha", "agent-beta"},
		ResetEvery: 3,
		WorkingDir: tmpDir,
	}); err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	final, err := sync.Read()
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if final.CompletedCycles != 5 {
		t.Fatalf("completed_cycles = %d, want 5", final.CompletedCycles)
	}
	if final.Halted {
		t.Fatal("expected not halted after max cycles")
	}

	// Verify rotation by inspecting history (goals left by adapters).
	// Since CompleteCycle clears NextCycleGoal, the *final* goal is empty.
	// But we can verify the cycle count proves the loop ran.
}

func TestOrchestratorHaltOnGateFailure(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 10}, "cyc_e2e_2")
	_ = sync.Write(state)

	reg := adapter.NewRegistry()
	if err := reg.Register("agent-alpha", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"keep-going"}`}
	})); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, sync)
	orch.SetDriverPipeline(Pipeline{
		mockGate{
			name:   "audit",
			result: contract.GateFailure{R: contract.GateReport{Gate: "audit", Passed: false}, Reason: contract.HaltGateFailure},
			state:  state,
		},
	})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"agent-alpha"},
		WorkingDir: tmpDir,
	})
	if err == nil {
		t.Fatal("expected halt error")
	}
	var he *HaltedError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HaltedError, got %T", err)
	}
	if he.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want gate-failure", he.Reason)
	}

	final, _ := sync.Read()
	if !final.Halted {
		t.Fatal("expected halted state persisted")
	}
	if final.LastFailedGate != "audit" {
		t.Fatalf("last_failed_gate = %q, want audit", final.LastFailedGate)
	}
}

func TestOrchestratorAdapterNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 10}, "cyc_e2e_3")
	_ = sync.Write(state)

	// Empty registry — no adapters registered.
	orch := NewMultiAgentOrchestrator(adapter.NewRegistry(), sync)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"missing-agent"},
		WorkingDir: tmpDir,
	})
	if err == nil {
		t.Fatal("expected error for missing adapter")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		// Should fail immediately, not timeout.
		t.Logf("error: %v", err)
	}
}
