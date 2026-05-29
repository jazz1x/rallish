package contract

// replay.go — G4+ replay / determinism: a PURE reader that reconstructs a run's
// control-flow graph from an already-loaded []HarnessLedgerEntry.
//
// This is the audit/consumer (query) layer of the Work Graph, NOT a runtime: it
// never executes, re-runs, schedules, or mutates anything. It traverses the typed
// append-only ledger — whose linear prev_hash chain already makes it a DAG — and
// reconstructs the LangGraph-style control graph (ordered nodes = phases / agent
// turns / gates; edges = event sequence + handoff targets), segmented per cycle,
// so a consumer can deterministically replay "what happened".
//
// VERIFY-BEFORE-RECONSTRUCT: Replay calls VerifyChain first. A replay over a
// TAMPERED ledger must NOT silently reconstruct — if the chain is broken Replay
// returns the broken index and an error, refusing to produce a graph from
// untrustworthy provenance. The single writer (LedgerFileSync.Append) populates
// prev_hash/hash on EVERY entry, so a real on-disk ledger is fully chained; an
// un-chained slice (empty prev_hash on the head) fails VerifyChain and is rejected
// — exactly the intended behaviour. ReplayVerified is the escape hatch for a
// caller that has already verified this exact slice and wants to skip the re-walk.
//
// It is deliberately NOT subgraph isomorphism, a graph DB, or a GNN — the graph is
// a lens (north-star non-goals). The runtime stays a typed append-only log; this
// reader is cheap and O(n) in len(entries).

import (
	"errors"
	"fmt"
)

// ErrTamperedLedger is returned by Replay/ReplayVerified when the hash chain does
// not verify, so a consumer can distinguish a tamper rejection from a malformed
// call. Use BrokenLinkError to recover the offending index.
var ErrTamperedLedger = errors.New("contract: ledger chain broken; refusing to reconstruct control graph")

// BrokenLinkError wraps ErrTamperedLedger with the index of the first entry whose
// hash chain link is broken (as reported by VerifyChain). A replay over a tampered
// ledger fails closed and cites exactly where trust was lost.
type BrokenLinkError struct {
	// Index is the position of the first entry that failed the chain check; it
	// matches VerifyChain's brokenIndex semantics.
	Index int
}

func (e *BrokenLinkError) Error() string {
	return fmt.Sprintf("contract: ledger chain broken at index %d; refusing to reconstruct control graph", e.Index)
}

// Unwrap lets errors.Is(err, ErrTamperedLedger) match a *BrokenLinkError.
func (e *BrokenLinkError) Unwrap() error { return ErrTamperedLedger }

// EdgeKind labels why one Step points at another in the reconstructed graph.
type EdgeKind string

const (
	// EdgeSequence is the implicit edge from one event to the next event WITHIN
	// the same cycle, in append order — the control-flow backbone.
	EdgeSequence EdgeKind = "sequence"
	// EdgeHandoff is the edge created by a handoff_created event carrying a
	// HandoffTo target: it points at the first subsequent step (in append order)
	// whose Agent matches that target, modelling the baton change. If no later
	// step matches the target, no handoff edge is emitted (the handoff was the
	// terminal act of the trace).
	EdgeHandoff EdgeKind = "handoff"
)

// Edge is a directed edge in the reconstructed control graph, identified by the
// destination Step index plus the reason the edge exists. Edges are an O(n) view
// over the ledger order; they are NOT a general adjacency structure for graph
// algorithms (no isomorphism / GNN — see north-star non-goals).
type Edge struct {
	// To is the index, within ControlGraph.Steps, of the destination step.
	To int `json:"to"`
	// Kind is why this edge exists (sequence backbone or handoff target).
	Kind EdgeKind `json:"kind"`
}

// Step is one node in the reconstructed control graph: a single ledger event,
// projected to the fields a consumer needs to traverse and label the run. It is a
// reader projection of HarnessLedgerEntry — it copies, it does not alias, the
// underlying entry, and it adds no information the ledger did not already record.
type Step struct {
	// Index is this step's position in ControlGraph.Steps; it equals the entry's
	// position in the input slice, so a consumer can cross-reference the source.
	Index int `json:"index"`
	// CycleID is the cycle this step belongs to, copied from the entry.
	CycleID string `json:"cycle_id"`
	// Type is the ledger event type for this step (the node's kind).
	Type LedgerEventType `json:"type"`
	// At is the entry's Unix-millisecond timestamp, preserved for ordering display.
	At int64 `json:"at"`
	// Agent is the optional runtime/role responsible for the event.
	Agent string `json:"agent,omitempty"`
	// Gate is the optional gate name for gate_passed/gate_failed steps.
	Gate string `json:"gate,omitempty"`
	// HandoffTo is the optional baton target named by a handoff_created step.
	HandoffTo string `json:"handoff_to,omitempty"`
	// Summary is the compact human-readable event summary, copied from the entry.
	Summary string `json:"summary,omitempty"`
	// Out lists this step's outgoing edges (sequence backbone first, then any
	// handoff edge), in a deterministic order so two replays of the same ledger
	// produce byte-identical graphs.
	Out []Edge `json:"out,omitempty"`
}

// CycleSegment groups the contiguous-by-id steps of a single cycle. Cycles are
// listed in first-seen append order. Begin/End are inclusive step-index bounds of
// the first and last step bearing this CycleID; a consumer can slice
// ControlGraph.Steps[Begin:End+1] only when the cycle's events are contiguous,
// which they are for a well-formed single-writer ledger — Steps carries the
// authoritative per-step CycleID regardless.
type CycleSegment struct {
	// CycleID identifies the cycle.
	CycleID string `json:"cycle_id"`
	// Steps holds the indices (into ControlGraph.Steps) of every step in this
	// cycle, in append order. Using explicit indices (not a Begin:End range) keeps
	// segmentation correct even if a future ledger interleaves cycles.
	Steps []int `json:"steps"`
	// Terminal is the type of the cycle's last step (cycle_completed, cycle_halted,
	// or whatever the trace ended on) so a consumer can classify the outcome
	// without re-scanning. Empty only for an empty segment, which never occurs.
	Terminal LedgerEventType `json:"terminal,omitempty"`
}

// ControlGraph is the deterministic reconstruction of a run from its ledger: an
// ordered list of Steps (the nodes, with their outgoing edges) plus a per-cycle
// segmentation. It is produced only over a chain that VerifyChain accepted, so a
// consumer can trust that the reconstruction reflects untampered provenance.
type ControlGraph struct {
	// Steps are the reconstructed nodes in append order; Steps[i].Index == i.
	Steps []Step `json:"steps"`
	// Cycles segments Steps by CycleID in first-seen order.
	Cycles []CycleSegment `json:"cycles"`
}

// Replay verifies the ledger's hash chain and, only if intact, reconstructs the
// run's control graph. It is the convenience entry point that ALWAYS enforces
// verify-before-reconstruct: a tampered ledger yields a *BrokenLinkError (which
// errors.Is matches ErrTamperedLedger) citing the broken index, and a nil graph.
//
// An empty (or nil) ledger verifies trivially and yields an empty, non-nil graph.
// Replay is pure: no I/O, no goroutines, O(n) in len(entries), no new deps.
func Replay(entries []HarnessLedgerEntry) (ControlGraph, error) {
	if brokenIndex, ok := VerifyChain(entries); !ok {
		return ControlGraph{}, &BrokenLinkError{Index: brokenIndex}
	}
	return reconstruct(entries), nil
}

// ReplayVerified reconstructs the control graph WITHOUT re-running VerifyChain,
// for a caller that has already verified the same slice and wants to avoid a
// redundant O(n) hash recomputation (e.g. a broker that verified on readback).
// The caller is responsible for having verified the chain; passing an unverified
// or tampered ledger reconstructs from untrustworthy data — prefer Replay unless
// the verification is provably already done on this exact slice.
func ReplayVerified(entries []HarnessLedgerEntry) ControlGraph {
	return reconstruct(entries)
}

// reconstruct is the shared O(n) builder. It does NO verification — both public
// entry points decide the trust policy and delegate here so the reconstruction
// logic is single-sourced.
func reconstruct(entries []HarnessLedgerEntry) ControlGraph {
	graph := ControlGraph{
		Steps:  make([]Step, 0, len(entries)),
		Cycles: nil,
	}
	if len(entries) == 0 {
		return graph
	}

	// First pass: project entries to Steps and group indices by cycle in
	// first-seen order. cycleAt maps a CycleID to its slot in graph.Cycles.
	cycleAt := make(map[string]int, len(entries))
	// lastIdxInCycle tracks the previous step index for each cycle so we can add
	// the sequence backbone edge (within-cycle, append order) in this same pass.
	lastIdxInCycle := make(map[string]int, len(entries))

	for i, e := range entries {
		step := Step{
			Index:     i,
			CycleID:   e.CycleID,
			Type:      e.Type,
			At:        e.At,
			Agent:     e.Agent,
			Gate:      e.Gate,
			HandoffTo: e.HandoffTo,
			Summary:   e.Summary,
		}
		graph.Steps = append(graph.Steps, step)

		// Sequence backbone: link the previous step of THIS cycle to this one.
		if prevIdx, seen := lastIdxInCycle[e.CycleID]; seen {
			graph.Steps[prevIdx].Out = append(graph.Steps[prevIdx].Out, Edge{To: i, Kind: EdgeSequence})
		}
		lastIdxInCycle[e.CycleID] = i

		// Per-cycle segmentation in first-seen order.
		if seg, seen := cycleAt[e.CycleID]; seen {
			graph.Cycles[seg].Steps = append(graph.Cycles[seg].Steps, i)
		} else {
			cycleAt[e.CycleID] = len(graph.Cycles)
			graph.Cycles = append(graph.Cycles, CycleSegment{
				CycleID: e.CycleID,
				Steps:   []int{i},
			})
		}
	}

	// Second pass: handoff edges. A handoff_created step with a HandoffTo target
	// points at the FIRST later step whose Agent equals that target (the baton
	// pickup). This is the cross-cycle / cross-agent edge the sequence backbone
	// cannot express. O(n) overall because each handoff scans forward only until
	// its pickup and stops — bounded by the per-handoff lookahead helper below,
	// kept simple and deterministic.
	addHandoffEdges(graph.Steps, entries)

	// Record each cycle's terminal step type for outcome classification.
	for c := range graph.Cycles {
		seg := &graph.Cycles[c]
		lastStep := seg.Steps[len(seg.Steps)-1]
		seg.Terminal = graph.Steps[lastStep].Type
	}

	return graph
}

// addHandoffEdges adds an EdgeHandoff from every handoff_created step that names a
// HandoffTo target to the first SUBSEQUENT step whose Agent matches that target.
// If no later step picks up the baton, no edge is added (the handoff was terminal).
// Determinism: it always picks the earliest matching index, so repeated replays of
// the same ledger emit identical edges.
func addHandoffEdges(steps []Step, entries []HarnessLedgerEntry) {
	for i := range steps {
		if entries[i].Type != LedgerEventHandoffCreated {
			continue
		}
		target := entries[i].HandoffTo
		if target == "" {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Agent == target {
				steps[i].Out = append(steps[i].Out, Edge{To: j, Kind: EdgeHandoff})
				break
			}
		}
	}
}
