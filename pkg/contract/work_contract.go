package contract

// WorkContract is the vendor-neutral harness contract that tells any agent
// runtime what work is allowed, how it is bounded, and how it must be verified.
type WorkContract struct {
	// Objective is the one-sentence work goal the next agent should pursue.
	Objective string `json:"objective"`
	// RepoRoot is the repository root where the work should happen.
	RepoRoot string `json:"repo_root,omitempty"`
	// Branch is the target git branch for the work.
	Branch string `json:"branch,omitempty"`
	// PendingFiles scopes the agent toward known files without forbidding discovery.
	PendingFiles []string `json:"pending_files,omitempty"`
	// LocalGates are project-specific validation commands that must pass.
	LocalGates []string `json:"local_gates,omitempty"`
	// MaxCycles caps autonomous loop turns for safety.
	MaxCycles int `json:"max_cycles,omitempty"`
	// MaxDurationMinutes caps wall-clock runtime for safety.
	MaxDurationMinutes int `json:"max_duration_minutes,omitempty"`
	// AutoGoal allows the harness to discover the next bounded objective.
	AutoGoal bool `json:"auto_goal,omitempty"`
	// Orchestrator describes optional multi-agent rotation.
	Orchestrator *OrchestratorConfig `json:"orchestrator,omitempty"`
}

// WorkContract projects a cycle creation request into the stable harness contract.
func (r NewCycleRequest) WorkContract() WorkContract {
	repoRoot := ""
	if r.Orchestrator != nil {
		repoRoot = r.Orchestrator.WorkingDir
	}
	return WorkContract{
		Objective:          r.Goal,
		RepoRoot:           repoRoot,
		Branch:             r.Branch,
		PendingFiles:       append([]string(nil), r.PendingFiles...),
		LocalGates:         append([]string(nil), r.LocalGates...),
		MaxCycles:          r.MaxCycles,
		MaxDurationMinutes: r.MaxDurationMinutes,
		AutoGoal:           r.AutoGoal,
		Orchestrator:       r.Orchestrator,
	}
}

// WorkContract projects the current cycle state into the stable harness contract.
func (s CycleState) WorkContract(repoRoot string, orchestrator *OrchestratorConfig) WorkContract {
	return WorkContract{
		Objective:          s.NextCycleGoal,
		RepoRoot:           repoRoot,
		Branch:             s.Branch,
		PendingFiles:       append([]string(nil), s.PendingFiles...),
		LocalGates:         append([]string(nil), s.LocalGates...),
		MaxCycles:          s.MaxCycles,
		MaxDurationMinutes: s.MaxDurationMinutes,
		AutoGoal:           s.AutoGoal,
		Orchestrator:       orchestrator,
	}
}
