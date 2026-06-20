package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/adapter/fake"
	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/internal/cycle/gates"
	"github.com/jazz1x/rallish/pkg/contract"
)

// passCLIGate is a deterministic pass-through gate so the one-shot tests do not
// depend on the git-backed standard pipeline.
type passCLIGate struct{ name string }

func (g passCLIGate) Name() string { return g.name }
func (g passCLIGate) Run(_ context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	return contract.GateSuccess{R: contract.GateReport{Gate: g.name, Passed: true}}, state
}

// failCLIGate halts with a configured reason. stderr is optional: when set it
// populates the failing report's Stderr (the human "why" the halt path should
// surface); the zero value leaves it empty, matching a gate that reports no detail.
type failCLIGate struct {
	name   string
	reason contract.HaltReason
	stderr string
}

func (g failCLIGate) Name() string { return g.name }
func (g failCLIGate) Run(_ context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	return contract.GateFailure{R: contract.GateReport{Gate: g.name, Passed: false, Stderr: g.stderr}, Reason: g.reason}, state
}

func passPipeline(_ cycle.State) cycle.Pipeline {
	return cycle.Pipeline{passCLIGate{name: "ok"}}
}

func writeCycle(t *testing.T, dir, id string, req contract.NewCycleRequest) *cycle.StateFileSync {
	t.Helper()
	statePath := filepath.Join(dir, "cycle-"+id+".json")
	sync := cycle.NewStateFileSync(statePath)
	state, err := cycle.NewState(req, id)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := sync.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return sync
}

// TestExitCodeForHaltSSOT is the single-source-of-truth guard: every known halt
// reason maps to a stable, distinct code, success maps to 0, and an unknown
// reason degrades to the forward-compat code (never a panic / silent 0).
func TestExitCodeForHaltSSOT(t *testing.T) {
	cases := map[contract.HaltReason]int{
		contract.HaltSuccess:            exitCleanPass,
		contract.HaltStuck:              exitHaltStuck,
		contract.HaltBudgetExceeded:     exitHaltBudget,
		contract.HaltPreflightFailed:    exitHaltPreflight,
		contract.HaltGateFailure:        exitHaltGateFailure,
		contract.HaltUnparseableTurn:    exitHaltUnparseable,
		contract.HaltUserRequested:      exitHaltUserRequested,
		contract.HaltSelfAuditViolation: exitHaltSelfAudit,
		contract.HaltSSHAuthFailed:      exitHaltSSHAuth,
		contract.HaltMaxCyclesReached:   exitHaltMaxCycles,
		contract.HaltReason("nonsense"): exitHaltUnknownReason,
	}
	seen := map[int]contract.HaltReason{}
	for reason, want := range cases {
		got := exitCodeForHalt(reason)
		if got != want {
			t.Errorf("exitCodeForHalt(%q) = %d, want %d", reason, got, want)
		}
		// Distinctness among the explicitly-mapped non-zero codes.
		if got != exitCleanPass {
			if prev, dup := seen[got]; dup && prev != reason {
				t.Errorf("exit code %d shared by %q and %q (must be distinct)", got, prev, reason)
			}
			seen[got] = reason
		}
	}
}

// TestRunCycleOnceCleanPass is the false-positive guard: a normal advanceable
// cycle runs ONE step and exits 0 without spuriously halting.
func TestRunCycleOnceCleanPass(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_clean"
	sync := writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: passPipeline,
	}, &out)
	if err != nil {
		t.Fatalf("expected clean pass (nil error), got %v", err)
	}

	final, _ := sync.Read()
	if final.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", final.CompletedCycles)
	}
	if final.Halted {
		t.Fatal("a clean advanceable cycle must not halt")
	}
}

// TestRunCycleOnceGateFailureExitCode: a Driver.Step gate failure halts with the
// gate-failure code and seals a cycle_halted ledger entry.
func TestRunCycleOnceGateFailureExitCode(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_gatefail"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: func(_ cycle.State) cycle.Pipeline {
			return cycle.Pipeline{failCLIGate{name: "audit", reason: contract.HaltGateFailure}}
		},
	}, &out)

	assertExitCode(t, err, exitHaltGateFailure)

	// The halt must be sealed in the ledger for the next invocation's reviver guard.
	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	entries, _ := ledger.ReadAll()
	if reason, sealed := cycle.LedgerSealsResume(entries); !sealed || reason != contract.HaltGateFailure {
		t.Fatalf("ledger not sealed with gate-failure: reason=%q sealed=%v", reason, sealed)
	}
}

// TestRunCycleOnceHaltSurfacesGateReason guards the user-facing "why": when a gate
// fails, the failing report's Stderr (e.g. the preflight reason `branch "main" is
// forbidden`) must reach stdout alongside the reason+code, not be discarded. All
// preflight failure classes otherwise collapse to the opaque `preflight-failed`.
func TestRunCycleOnceHaltSurfacesGateReason(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_why"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	const why = `branch "main" is forbidden`
	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: func(_ cycle.State) cycle.Pipeline {
			return cycle.Pipeline{failCLIGate{name: "preflight", reason: contract.HaltPreflightFailed, stderr: why}}
		},
	}, &out)

	// Behaviour/exit-code unchanged: still the documented preflight code.
	assertExitCode(t, err, exitHaltPreflight)
	if !strings.Contains(out.String(), why) {
		t.Fatalf("halt output dropped the failing gate's reason; got:\n%s", out.String())
	}
}

// TestRunCycleOnceHaltNoGateDetailIsQuiet is the false-positive guard for the
// reason line: a halt whose failing report carries NO Stderr must not print a
// spurious empty `reason:` line — the extra line appears only when there is a why.
func TestRunCycleOnceHaltNoGateDetailIsQuiet(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_nowhy"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: func(_ cycle.State) cycle.Pipeline {
			return cycle.Pipeline{failCLIGate{name: "audit", reason: contract.HaltGateFailure}}
		},
	}, &out)

	assertExitCode(t, err, exitHaltGateFailure)
	if strings.Contains(out.String(), "reason:") {
		t.Fatalf("halt with no gate detail printed a spurious reason line; got:\n%s", out.String())
	}
}

// TestFailedStderr pins the detail-selection contract: the LAST failing report's
// Stderr wins (the gate that short-circuited the pipeline), with a fallback to the
// last report and an empty string for no reports (no fabricated detail).
func TestFailedStderr(t *testing.T) {
	if got := failedStderr(nil); got != "" {
		t.Fatalf("empty reports: got %q, want \"\"", got)
	}
	reports := []contract.GateReport{
		{Gate: "preflight", Passed: true},
		{Gate: "audit", Passed: false, Stderr: "make check failed"},
	}
	if got := failedStderr(reports); got != "make check failed" {
		t.Fatalf("last failing report: got %q, want %q", got, "make check failed")
	}
	// No report marked !Passed → fall back to the last report's Stderr.
	allPass := []contract.GateReport{
		{Gate: "preflight", Passed: true, Stderr: "warn-a"},
		{Gate: "audit", Passed: true, Stderr: "warn-b"},
	}
	if got := failedStderr(allPass); got != "warn-b" {
		t.Fatalf("fallback to last report: got %q, want %q", got, "warn-b")
	}
}

// TestRunCycleOnceStuckExitCode: a pre-seeded stuck ledger halts the pass with
// the stuck code BEFORE running the step (the adapter/agent work is irrelevant).
func TestRunCycleOnceStuckExitCode(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_stuck"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 10})

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	stuck := contract.TurnResponse{Summary: "same"}
	for i := 0; i < cycle.StuckRepeatTurnThreshold; i++ {
		if err := ledger.Append(contract.NewAgentTurnLedgerEntry(int64(1000+i), id, "builder", stuck)); err != nil {
			t.Fatalf("seed ledger: %v", err)
		}
	}

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		// Pipeline would advance if reached — it must NOT be reached.
		pipeline: passPipeline,
	}, &out)

	assertExitCode(t, err, exitHaltStuck)
}

// TestRunCycleOnceBudgetExitCode: a productive runaway (novel turns, never
// stuck) trips the hard lifetime ceiling and exits with the budget code.
func TestRunCycleOnceBudgetExitCode(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_budget"
	const ceiling = 3
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 100})

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	for i := 0; i < ceiling; i++ {
		resp := contract.TurnResponse{Summary: "novel-" + string(rune('a'+i))}
		if err := ledger.Append(contract.NewAgentTurnLedgerEntry(int64(1000+i), id, "builder", resp)); err != nil {
			t.Fatalf("seed ledger: %v", err)
		}
	}

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:          id,
		stateDir:         dir,
		maxLifetimeTurns: ceiling,
		pipeline:         passPipeline,
	}, &out)

	assertExitCode(t, err, exitHaltBudget)
}

// TestRunCycleOnceRefusesSealedResume: a ledger already sealed by a sticky halt
// (no later validation_green) refuses to revive and exits with the sticky code.
// This is the cron-resume safety: the harness halts, an external driver may
// re-trigger, and THIS pass refuses rather than spinning.
func TestRunCycleOnceRefusesSealedResume(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_sealed"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 10})

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	_ = ledger.Append(contract.NewAgentTurnLedgerEntry(1000, id, "builder", contract.TurnResponse{Summary: "work"}))
	_ = ledger.Append(contract.NewHarnessLedgerEntry(1001, id, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil))

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: passPipeline,
	}, &out)

	assertExitCode(t, err, exitHaltStuck)
}

// TestRunCycleOnceResumesAfterProgress is the second false-positive guard for the
// reviver: a halt FOLLOWED by a validation_green lifts the seal, so the pass runs
// its step and exits 0 (measurable progress permits revival).
func TestRunCycleOnceResumesAfterProgress(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_resumed"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 10})

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	_ = ledger.Append(contract.NewHarnessLedgerEntry(1000, id, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil))
	_ = ledger.Append(contract.NewHarnessLedgerEntry(1001, id, contract.LedgerEventValidationGreen, "tests green", nil))

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		pipeline: passPipeline,
	}, &out)
	if err != nil {
		t.Fatalf("progress after halt must permit a clean pass, got %v", err)
	}
}

// TestRunCycleOnceAlreadyHalted: a persisted halted state reports its sticky
// reason and exit code without re-running.
func TestRunCycleOnceAlreadyHalted(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_halted"
	sync := writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 10})
	state, _ := sync.Read()
	_ = state.Halt(contract.HaltUserRequested)
	if err := sync.Write(state); err != nil {
		t.Fatalf("write halted: %v", err)
	}

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{cycleID: id, stateDir: dir, pipeline: passPipeline}, &out)
	assertExitCode(t, err, exitHaltUserRequested)
}

// TestRunCycleOnceNotAdvanceableIsCleanExit: a cycle that reached max cycles is
// terminal-but-not-failure → exit 0, no halt write.
func TestRunCycleOnceNotAdvanceableIsCleanExit(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_maxed"
	sync := writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 2})
	state, _ := sync.Read()
	state.CompletedCycles = 2 // at the cap
	if err := sync.Write(state); err != nil {
		t.Fatalf("write maxed: %v", err)
	}

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{cycleID: id, stateDir: dir, pipeline: passPipeline}, &out)
	if err != nil {
		t.Fatalf("not-advanceable cycle should exit 0, got %v", err)
	}
}

// TestRunCycleOnceMissingCycleErrors: an unknown cycle is an operational error
// (generic exit 1 path), NOT a halt code.
func TestRunCycleOnceMissingCycleErrors(t *testing.T) {
	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{cycleID: "nope", stateDir: t.TempDir(), pipeline: passPipeline}, &out)
	if err == nil {
		t.Fatal("expected error for missing cycle")
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		t.Fatalf("missing cycle must be a plain operational error, not an exit-coded halt (got code %d)", coded.ExitCode())
	}
}

// TestCycleRunRequiresOnce: invoking `cycle run` without --once is rejected (the
// command is bounded-only; there is no watch/loop mode).
func TestCycleRunRequiresOnce(t *testing.T) {
	cmd := CycleRunCmd()
	cmd.SetArgs([]string{"--cycle-id", "cyc_x"}) // no --once
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when --once is omitted")
	}
}

// assertExitCode checks that err is an *exitCodeError with the wanted code.
func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an exit-coded halt (%d), got nil", want)
	}
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) {
		t.Fatalf("expected an *exitCodeError, got %T: %v", err, err)
	}
	if coded.ExitCode() != want {
		t.Fatalf("exit code = %d, want %d (err: %v)", coded.ExitCode(), want, err)
	}
}

// fakeRegistry returns a buildRegistry func that registers a single fake adapter
// under name, so the agent-driven one-shot tests stay offline and deterministic
// (mirroring how pipeline is injected — the real default is cli.BuildRegistry).
func fakeRegistry(name string, fn func(turn int) contract.TurnResponse) func() (*adapter.Registry, error) {
	return func() (*adapter.Registry, error) {
		reg := adapter.NewRegistry()
		if err := reg.Register(name, fake.New(fn)); err != nil {
			return nil, err
		}
		return reg, nil
	}
}

// TestRunCycleOnceAgentCleanPass is the agent-mode false-positive guard: with a
// well-behaved adapter (valid-JSON handshake) the pass drives one turn, advances
// one cycle via the shared orchestrator iteration, and exits 0.
func TestRunCycleOnceAgentCleanPass(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_agent_clean"
	sync := writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		agents:   []string{"builder"},
		pipeline: passPipeline,
		buildRegistry: fakeRegistry("builder", func(turn int) contract.TurnResponse {
			return contract.TurnResponse{Summary: fmt.Sprintf(`{"next_goal":"turn-%d"}`, turn)}
		}),
	}, &out)
	if err != nil {
		t.Fatalf("expected clean agent pass (nil error), got %v", err)
	}

	final, _ := sync.Read()
	if final.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", final.CompletedCycles)
	}
	if final.Halted {
		t.Fatal("a clean agent pass must not halt")
	}
}

// TestRunCycleOnceAgentUnparseableHalts: the strict handshake holds in the local
// one-shot too — a non-JSON turn halts with the unparseable code and seals a
// cycle_halted entry (via the orchestrator's handleHandshakeHalt) so the next
// invocation's reviver guard stays sticky.
func TestRunCycleOnceAgentUnparseableHalts(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_agent_unparseable"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		agents:   []string{"builder"},
		pipeline: passPipeline,
		buildRegistry: fakeRegistry("builder", func(_ int) contract.TurnResponse {
			return contract.TurnResponse{Summary: "prose, not the typed handshake"}
		}),
	}, &out)

	assertExitCode(t, err, exitHaltUnparseable)

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	entries, _ := ledger.ReadAll()
	if reason, sealed := cycle.LedgerSealsResume(entries); !sealed || reason != contract.HaltUnparseableTurn {
		t.Fatalf("ledger not sealed with unparseable: reason=%q sealed=%v", reason, sealed)
	}
}

// TestRunCycleOnceAgentStuckHaltsBeforeTurn: in agent mode the G5 anti-spin guard
// (inside the shared RunOnce) still fires BEFORE the agent turn — a pre-seeded
// stuck ledger halts with the stuck code and the adapter is never invoked.
func TestRunCycleOnceAgentStuckHaltsBeforeTurn(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_agent_stuck"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 10})

	ledger := cycle.NewLedgerFileSync(filepath.Join(dir, "cycle-"+id+"-ledger.jsonl"))
	stuck := contract.TurnResponse{Summary: "same"}
	for i := 0; i < cycle.StuckRepeatTurnThreshold; i++ {
		if err := ledger.Append(contract.NewAgentTurnLedgerEntry(int64(1000+i), id, "builder", stuck)); err != nil {
			t.Fatalf("seed ledger: %v", err)
		}
	}

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		agents:   []string{"builder"},
		pipeline: passPipeline,
		buildRegistry: fakeRegistry("builder", func(_ int) contract.TurnResponse {
			t.Error("adapter must NOT run when the anti-spin guard halts first")
			return contract.TurnResponse{}
		}),
	}, &out)

	assertExitCode(t, err, exitHaltStuck)
}

// TestRunCycleOnceAgentUnknownAdapterIsOperationalError: an --agents name that
// does not resolve in the registry is an operational error (generic exit 1), not
// an exit-coded halt — the orchestrator's "adapter not found" surfaces plainly.
func TestRunCycleOnceAgentUnknownAdapterIsOperationalError(t *testing.T) {
	dir := t.TempDir()
	const id = "cyc_agent_unknown"
	writeCycle(t, dir, id, contract.NewCycleRequest{Goal: "feat: work", MaxCycles: 5})

	var out bytes.Buffer
	err := runCycleOnce(context.Background(), runOnceOptions{
		cycleID:  id,
		stateDir: dir,
		agents:   []string{"nonesuch"},
		pipeline: passPipeline,
		buildRegistry: fakeRegistry("builder", func(_ int) contract.TurnResponse {
			return contract.TurnResponse{Summary: `{"next_goal":"x"}`}
		}),
	}, &out)
	if err == nil {
		t.Fatal("expected an error for an unknown adapter")
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		t.Fatalf("unknown adapter must be a plain operational error, not an exit-coded halt (got code %d)", coded.ExitCode())
	}
}

// TestBuildCLIPipelineGoldenOrder pins the CLI one-shot's canonical gate order,
// including repo-local command gates inserted after audit. It is the CLI-side
// half of the single golden assertion shared with the broker
// (TestPipelineForStateInsertsLocalGatesAfterAudit): both delegate to
// gates.StandardPipeline, so this guards against a gate being dropped or
// reordered in the shared constructor or in the CLI delegation. The explicit
// `want` slice is the false-positive guard — it fails loudly on any drift rather
// than only checking the two builders agree with each other.
func TestBuildCLIPipelineGoldenOrder(t *testing.T) {
	state, err := cycle.NewState(contract.NewCycleRequest{
		Goal:       "feat: test cli pipeline order",
		LocalGates: []string{"go test ./...", "go vet ./..."},
	}, "cyc_cli_order")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	names := buildCLIPipeline(state).Names()
	want := []string{
		"preflight",
		"audit",
		"cmd:go test ./...",
		"cmd:go vet ./...",
		"philosophy",
		"polish",
		"commit",
	}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}

	// The CLI builder must be exactly the shared constructor — no divergent
	// wrapping. This ties both entry points to one source of truth.
	shared := gates.StandardPipeline(state.AuditCmd, state.PolishTestCmd, state.LocalGates).Names()
	if len(shared) != len(names) {
		t.Fatalf("buildCLIPipeline %v diverges from gates.StandardPipeline %v", names, shared)
	}
	for i := range names {
		if shared[i] != names[i] {
			t.Fatalf("buildCLIPipeline %v diverges from gates.StandardPipeline %v", names, shared)
		}
	}
}
