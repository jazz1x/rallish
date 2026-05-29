package contract

import (
	"errors"
	"testing"
)

// chainedRun builds a fully hash-chained ledger from a list of (cycleID, type,
// agent, handoffTo) tuples, exactly as LedgerFileSync.Append would (PrevHash =
// predecessor Hash, genesis for the head). Tests stay pure and in-package, with no
// internal/cycle import, mirroring buildChain in harness_ledger_test.go.
func chainedRun(t *testing.T, events []HarnessLedgerEntry) []HarnessLedgerEntry {
	t.Helper()
	out := make([]HarnessLedgerEntry, 0, len(events))
	prev := LedgerGenesisHash
	for i, e := range events {
		e.SchemaVersion = LedgerSchemaVersion
		e.PrevHash = prev
		hash, err := ChainHash(e, prev)
		if err != nil {
			t.Fatalf("chain hash %d: %v", i, err)
		}
		e.Hash = hash
		out = append(out, e)
		prev = hash
	}
	return out
}

// twoCycleRun is a representative intact multi-cycle ledger: cycle one runs an
// agent turn, fails then passes a gate, records validation_green, then hands off
// to a second agent which is created as cycle two, takes its turn, passes its gate
// and completes. It exercises every reconstruction concern: sequence backbone,
// per-cycle grouping, a cross-cycle handoff edge, and terminal classification.
func twoCycleRun(t *testing.T) []HarnessLedgerEntry {
	t.Helper()
	return chainedRun(t, []HarnessLedgerEntry{
		{At: 1, CycleID: "cyc_1", Type: LedgerEventCycleCreated, Summary: "start"},
		{At: 2, CycleID: "cyc_1", Type: LedgerEventAgentTurn, Agent: "builder", Summary: "impl"},
		{At: 3, CycleID: "cyc_1", Type: LedgerEventGateFailed, Gate: "test", Summary: "red"},
		{At: 4, CycleID: "cyc_1", Type: LedgerEventGatePassed, Gate: "test", Summary: "green"},
		{At: 5, CycleID: "cyc_1", Type: LedgerEventValidationGreen, Summary: "verified"},
		{At: 6, CycleID: "cyc_1", Type: LedgerEventHandoffCreated, Agent: "builder", HandoffTo: "reviewer", Summary: "handoff"},
		{At: 7, CycleID: "cyc_2", Type: LedgerEventCycleCreated, Summary: "start2"},
		{At: 8, CycleID: "cyc_2", Type: LedgerEventAgentTurn, Agent: "reviewer", Summary: "review"},
		{At: 9, CycleID: "cyc_2", Type: LedgerEventGatePassed, Gate: "audit", Summary: "ok"},
		{At: 10, CycleID: "cyc_2", Type: LedgerEventCycleCompleted, Summary: "done"},
	})
}

func TestReplayReconstructsOrderedSteps(t *testing.T) {
	entries := twoCycleRun(t)

	graph, err := Replay(entries)
	if err != nil {
		t.Fatalf("intact ledger: unexpected error %v", err)
	}
	if len(graph.Steps) != len(entries) {
		t.Fatalf("steps = %d, want %d", len(graph.Steps), len(entries))
	}
	// Steps preserve append order, index, and the projected fields.
	for i, e := range entries {
		s := graph.Steps[i]
		if s.Index != i {
			t.Fatalf("step %d Index = %d", i, s.Index)
		}
		if s.Type != e.Type {
			t.Fatalf("step %d type = %q, want %q", i, s.Type, e.Type)
		}
		if s.CycleID != e.CycleID {
			t.Fatalf("step %d cycle = %q, want %q", i, s.CycleID, e.CycleID)
		}
		if s.At != e.At {
			t.Fatalf("step %d at = %d, want %d", i, s.At, e.At)
		}
		if s.Agent != e.Agent || s.Gate != e.Gate || s.HandoffTo != e.HandoffTo {
			t.Fatalf("step %d field mismatch: %+v vs entry %+v", i, s, e)
		}
	}
}

func TestReplaySequenceBackboneIsPerCycle(t *testing.T) {
	graph, err := Replay(twoCycleRun(t))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	// Within cyc_1, steps 0..5 chain by sequence: 0->1->2->3->4->5.
	for i := 0; i <= 4; i++ {
		if !hasEdge(graph.Steps[i].Out, i+1, EdgeSequence) {
			t.Fatalf("missing sequence edge %d->%d", i, i+1)
		}
	}
	// The last step of cyc_1 (index 5, the handoff) has NO sequence successor —
	// cyc_2 starts a fresh backbone, so 5 must not sequence-link to 6.
	if hasEdge(graph.Steps[5].Out, 6, EdgeSequence) {
		t.Fatal("sequence edge crossed a cycle boundary (5->6)")
	}
	// cyc_2 backbone: 6->7->8->9.
	for i := 6; i <= 8; i++ {
		if !hasEdge(graph.Steps[i].Out, i+1, EdgeSequence) {
			t.Fatalf("missing cyc_2 sequence edge %d->%d", i, i+1)
		}
	}
}

func TestReplayHandoffEdgeTargetsPickupAgent(t *testing.T) {
	graph, err := Replay(twoCycleRun(t))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	// Step 5 is handoff_created HandoffTo="reviewer"; the first later step whose
	// Agent == "reviewer" is the cyc_2 agent_turn at index 7.
	if !hasEdge(graph.Steps[5].Out, 7, EdgeHandoff) {
		t.Fatalf("handoff step 5 missing handoff edge to 7; out = %+v", graph.Steps[5].Out)
	}
}

func TestReplayTerminalHandoffEmitsNoEdge(t *testing.T) {
	// A handoff whose target never appears later is the terminal act: no edge.
	entries := chainedRun(t, []HarnessLedgerEntry{
		{At: 1, CycleID: "cyc_1", Type: LedgerEventCycleCreated},
		{At: 2, CycleID: "cyc_1", Type: LedgerEventAgentTurn, Agent: "builder"},
		{At: 3, CycleID: "cyc_1", Type: LedgerEventHandoffCreated, Agent: "builder", HandoffTo: "ghost"},
	})
	graph, err := Replay(entries)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	for _, edge := range graph.Steps[2].Out {
		if edge.Kind == EdgeHandoff {
			t.Fatalf("dangling handoff produced an edge to %d", edge.To)
		}
	}
}

func TestReplaySegmentsByCycle(t *testing.T) {
	graph, err := Replay(twoCycleRun(t))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(graph.Cycles) != 2 {
		t.Fatalf("cycles = %d, want 2", len(graph.Cycles))
	}
	c1, c2 := graph.Cycles[0], graph.Cycles[1]
	if c1.CycleID != "cyc_1" || c2.CycleID != "cyc_2" {
		t.Fatalf("cycle order = %q,%q want cyc_1,cyc_2", c1.CycleID, c2.CycleID)
	}
	if len(c1.Steps) != 6 {
		t.Fatalf("cyc_1 steps = %v, want 6", c1.Steps)
	}
	if len(c2.Steps) != 4 {
		t.Fatalf("cyc_2 steps = %v, want 4", c2.Steps)
	}
	// Terminal classification: cyc_1 ended on a handoff, cyc_2 on completion.
	if c1.Terminal != LedgerEventHandoffCreated {
		t.Fatalf("cyc_1 terminal = %q, want handoff_created", c1.Terminal)
	}
	if c2.Terminal != LedgerEventCycleCompleted {
		t.Fatalf("cyc_2 terminal = %q, want cycle_completed", c2.Terminal)
	}
}

func TestReplayHaltedCycleTerminal(t *testing.T) {
	entries := chainedRun(t, []HarnessLedgerEntry{
		{At: 1, CycleID: "cyc_1", Type: LedgerEventCycleCreated},
		{At: 2, CycleID: "cyc_1", Type: LedgerEventAgentTurn, Agent: "builder"},
		{At: 3, CycleID: "cyc_1", Type: LedgerEventCycleHalted, Summary: "stuck"},
	})
	graph, err := Replay(entries)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if graph.Cycles[0].Terminal != LedgerEventCycleHalted {
		t.Fatalf("terminal = %q, want cycle_halted", graph.Cycles[0].Terminal)
	}
}

func TestReplayDeterministic(t *testing.T) {
	// Two replays of the same ledger must produce identical graphs (no map-order
	// leakage into Steps or edges).
	entries := twoCycleRun(t)
	a, err := Replay(entries)
	if err != nil {
		t.Fatalf("replay a: %v", err)
	}
	b, err := Replay(entries)
	if err != nil {
		t.Fatalf("replay b: %v", err)
	}
	if len(a.Steps) != len(b.Steps) || len(a.Cycles) != len(b.Cycles) {
		t.Fatal("graph shape differs between replays")
	}
	for i := range a.Steps {
		if len(a.Steps[i].Out) != len(b.Steps[i].Out) {
			t.Fatalf("step %d edge count differs: %d vs %d", i, len(a.Steps[i].Out), len(b.Steps[i].Out))
		}
		for k := range a.Steps[i].Out {
			if a.Steps[i].Out[k] != b.Steps[i].Out[k] {
				t.Fatalf("step %d edge %d differs: %+v vs %+v", i, k, a.Steps[i].Out[k], b.Steps[i].Out[k])
			}
		}
	}
}

func TestReplayEmptyLedger(t *testing.T) {
	graph, err := Replay(nil)
	if err != nil {
		t.Fatalf("empty ledger: unexpected error %v", err)
	}
	if len(graph.Steps) != 0 || len(graph.Cycles) != 0 {
		t.Fatalf("empty ledger produced %d steps / %d cycles", len(graph.Steps), len(graph.Cycles))
	}
}

func TestReplayRejectsTamperedLedger(t *testing.T) {
	entries := twoCycleRun(t)
	// Tamper with a middle entry's Summary after chaining; its stored Hash no
	// longer matches. VerifyChain flags index 3, so Replay must refuse to
	// reconstruct and cite exactly that index.
	entries[3].Summary = "tampered after the fact"

	graph, err := Replay(entries)
	if err == nil {
		t.Fatal("tampered ledger reconstructed without error")
	}
	if len(graph.Steps) != 0 {
		t.Fatalf("tampered ledger still produced %d steps", len(graph.Steps))
	}
	if !errors.Is(err, ErrTamperedLedger) {
		t.Fatalf("error %v does not match ErrTamperedLedger", err)
	}
	var ble *BrokenLinkError
	if !errors.As(err, &ble) {
		t.Fatalf("error %v is not a *BrokenLinkError", err)
	}
	if ble.Index != 3 {
		t.Fatalf("broken index = %d, want 3 (the tampered entry)", ble.Index)
	}
}

func TestReplayRejectsBrokenLinkage(t *testing.T) {
	entries := twoCycleRun(t)
	// Re-point a middle entry's PrevHash so linkage diverges; flagged at index 5.
	entries[5].PrevHash = LedgerGenesisHash

	_, err := Replay(entries)
	var ble *BrokenLinkError
	if !errors.As(err, &ble) || ble.Index != 5 {
		t.Fatalf("relinked ledger: got err %v, want BrokenLinkError at index 5", err)
	}
}

func TestReplayRejectsUnchainedLedger(t *testing.T) {
	// Entries built without the hash chain (empty prev_hash on the head) are not
	// trustworthy provenance; Replay must reject them rather than silently
	// reconstruct. This guards the writer invariant that every real entry is
	// chained.
	entries := []HarnessLedgerEntry{
		NewHarnessLedgerEntry(1, "cyc_1", LedgerEventCycleCreated, "start", nil),
		NewHarnessLedgerEntry(2, "cyc_1", LedgerEventAgentTurn, "impl", nil),
	}
	_, err := Replay(entries)
	if !errors.Is(err, ErrTamperedLedger) {
		t.Fatalf("un-chained ledger: got err %v, want ErrTamperedLedger", err)
	}
}

func TestReplayVerifiedSkipsCheck(t *testing.T) {
	// ReplayVerified reconstructs without re-verifying. On an intact ledger it
	// matches Replay; this is the false-positive guard for the no-verify path —
	// a normal multi-cycle ledger reconstructs cleanly.
	entries := twoCycleRun(t)
	got := ReplayVerified(entries)
	want, err := Replay(entries)
	if err != nil {
		t.Fatalf("Replay baseline: %v", err)
	}
	if len(got.Steps) != len(want.Steps) || len(got.Cycles) != len(want.Cycles) {
		t.Fatalf("ReplayVerified shape %d/%d != Replay %d/%d",
			len(got.Steps), len(got.Cycles), len(want.Steps), len(want.Cycles))
	}
}

// hasEdge reports whether edges contains an edge to dst with the given kind.
func hasEdge(edges []Edge, dst int, kind EdgeKind) bool {
	for _, e := range edges {
		if e.To == dst && e.Kind == kind {
			return true
		}
	}
	return false
}
