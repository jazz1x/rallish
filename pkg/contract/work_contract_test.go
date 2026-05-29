package contract

import "testing"

func TestNewCycleRequestWorkContract(t *testing.T) {
	req := NewCycleRequest{
		Goal:               "feat: harden harness",
		Branch:             "feat/harness",
		MaxCycles:          7,
		MaxDurationMinutes: 240,
		AutoGoal:           true,
		PendingFiles:       []string{"internal/cycle/state.go"},
		LocalGates:         []string{"make check-all"},
		Orchestrator: &OrchestratorConfig{
			Agents:     []string{"codex", "claude"},
			WorkingDir: "/repo",
		},
	}

	contract := req.WorkContract()
	if contract.Objective != req.Goal {
		t.Fatalf("objective = %q, want %q", contract.Objective, req.Goal)
	}
	if contract.RepoRoot != "/repo" {
		t.Fatalf("repo_root = %q, want /repo", contract.RepoRoot)
	}
	if contract.Branch != req.Branch {
		t.Fatalf("branch = %q, want %q", contract.Branch, req.Branch)
	}
	if len(contract.PendingFiles) != 1 || contract.PendingFiles[0] != req.PendingFiles[0] {
		t.Fatalf("pending_files = %v, want %v", contract.PendingFiles, req.PendingFiles)
	}
	if len(contract.LocalGates) != 1 || contract.LocalGates[0] != req.LocalGates[0] {
		t.Fatalf("local_gates = %v, want %v", contract.LocalGates, req.LocalGates)
	}
	if contract.Orchestrator == nil || contract.Orchestrator.WorkingDir != "/repo" {
		t.Fatalf("orchestrator = %#v", contract.Orchestrator)
	}

	req.PendingFiles[0] = "mutated.go"
	req.LocalGates[0] = "mutated"
	if contract.PendingFiles[0] == "mutated.go" || contract.LocalGates[0] == "mutated" {
		t.Fatalf("work contract should copy request slices")
	}
}

func TestCycleStateWorkContract(t *testing.T) {
	state := CycleState{
		NextCycleGoal:      "fix: continue harness",
		Branch:             "feat/harness",
		PendingFiles:       []string{"pkg/contract/work_contract.go"},
		LocalGates:         []string{"make check-all"},
		MaxCycles:          5,
		MaxDurationMinutes: 120,
		AutoGoal:           true,
	}
	orchestrator := &OrchestratorConfig{Agents: []string{"codex"}, WorkingDir: "/repo"}

	contract := state.WorkContract("/repo", orchestrator)
	if contract.Objective != state.NextCycleGoal {
		t.Fatalf("objective = %q, want %q", contract.Objective, state.NextCycleGoal)
	}
	if contract.RepoRoot != "/repo" {
		t.Fatalf("repo_root = %q, want /repo", contract.RepoRoot)
	}
	if contract.Orchestrator != orchestrator {
		t.Fatalf("orchestrator pointer was not preserved")
	}

	state.PendingFiles[0] = "mutated.go"
	state.LocalGates[0] = "mutated"
	if contract.PendingFiles[0] == "mutated.go" || contract.LocalGates[0] == "mutated" {
		t.Fatalf("work contract should copy cycle state slices")
	}
}
