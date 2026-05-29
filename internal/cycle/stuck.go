package cycle

// stuck.go — cheap, O(n) stuck detector over the harness ledger.
//
// These predicates are deliberately low-cost equality/counting checks, NOT
// subgraph isomorphism or semantic diffing. The only truly un-gameable signal
// remains the verifier-produced gate result; this detector is a *stuck* early-
// warning layer that fires on obvious repetition patterns before gates are
// reached.

import (
	"strings"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Thresholds for stuck detection. Exported so callers may document or override
// in tests without modifying this file.
const (
	// StuckRepeatTurnThreshold is the minimum number of identical agent-turn
	// fingerprints (Summary + Files) before the turn is considered stuck.
	StuckRepeatTurnThreshold = 4

	// StuckRepeatGateFailThreshold is the minimum count of gate-failed events
	// sharing the same (Gate, Summary) pair before it is considered stuck.
	StuckRepeatGateFailThreshold = 3

	// StuckPingPongThreshold is the minimum alternating-fingerprint run length
	// (A B A B …) in agent-turn events before ping-pong is declared.
	StuckPingPongThreshold = 6

	// StuckNoProgressWindow is the number of trailing agent-turn entries
	// examined when checking whether the file frontier advanced.
	StuckNoProgressWindow = 5
)

// turnFingerprint returns a compact string key for an agent-turn entry.
// It combines Summary and the sorted-stable Files slice so that two turns
// that touched the exact same files with the same summary hash the same way.
func turnFingerprint(e contract.HarnessLedgerEntry) string {
	return e.Summary + "\x00" + strings.Join(e.Files, "\x00")
}

// Stuck scans entries and returns (HaltStuck, true) when any of the four
// predicates trips, otherwise ("", false). It is a pure function: no I/O,
// no goroutines, O(n) in len(entries).
//
// Predicates:
//
//	a. Repeated turn   — same (Summary+Files) fingerprint >= StuckRepeatTurnThreshold times.
//	b. Repeated gate failure — same (Gate, Summary) pair >= StuckRepeatGateFailThreshold times.
//	c. Ping-pong       — agent-turn fingerprint sequence has an A,B,A,B… run >= StuckPingPongThreshold.
//	d. No progress     — the last StuckNoProgressWindow agent-turns introduced no new Files
//	                     fingerprint AND no LedgerEventValidationGreen appeared after their start.
func Stuck(entries []contract.HarnessLedgerEntry) (contract.HaltReason, bool) {
	// Collect agent-turn entries and build per-predicate counters in a single pass.

	type gateKey struct{ gate, summary string }

	// Counters used across the pass.
	turnCount := map[string]int{}      // fingerprint → count (predicate a)
	gateFailCount := map[gateKey]int{} // (gate,summary) → count (predicate b)
	var turnFingerprints []string      // ordered fingerprints (predicates c, d)
	var turnAt []int64                 // at-timestamps parallel to turnFingerprints (predicate d)

	for _, e := range entries {
		switch e.Type {
		case contract.LedgerEventAgentTurn:
			fp := turnFingerprint(e)
			turnCount[fp]++
			turnFingerprints = append(turnFingerprints, fp)
			turnAt = append(turnAt, e.At)

		case contract.LedgerEventGateFailed:
			k := gateKey{gate: e.Gate, summary: e.Summary}
			gateFailCount[k]++
		}
	}

	// --- predicate a: repeated turn ---
	for _, cnt := range turnCount {
		if cnt >= StuckRepeatTurnThreshold {
			return contract.HaltStuck, true
		}
	}

	// --- predicate b: repeated gate failure ---
	for _, cnt := range gateFailCount {
		if cnt >= StuckRepeatGateFailThreshold {
			return contract.HaltStuck, true
		}
	}

	// --- predicate c: ping-pong ---
	if pingPongStuck(turnFingerprints) {
		return contract.HaltStuck, true
	}

	// --- predicate d: no progress ---
	if noProgressStuck(entries, turnFingerprints, turnAt) {
		return contract.HaltStuck, true
	}

	return "", false
}

// pingPongStuck returns true when turnFingerprints contains an alternating
// A,B,A,B,… run (A != B) of length >= StuckPingPongThreshold.
func pingPongStuck(fps []string) bool {
	if len(fps) < StuckPingPongThreshold {
		return false
	}
	// Scan for alternating runs. A run starts when fps[i] != fps[i-1].
	runLen := 1
	for i := 1; i < len(fps); i++ {
		prev := fps[i-1]
		cur := fps[i]
		if cur != prev {
			// Check the alternating pattern: cur should equal fps[i-2] (if it exists).
			if i >= 2 && cur == fps[i-2] {
				runLen++
			} else {
				// Start a fresh alternating run of length 2.
				runLen = 2
			}
		} else {
			// Same fingerprint twice in a row breaks the alternating pattern.
			runLen = 1
		}
		if runLen >= StuckPingPongThreshold {
			return true
		}
	}
	return false
}

// noProgressStuck returns true when the last StuckNoProgressWindow agent-turn
// entries (a) introduced no new Files fingerprint relative to all prior turns,
// AND (b) no LedgerEventValidationGreen event appeared after the window start.
func noProgressStuck(
	all []contract.HarnessLedgerEntry,
	turnFPs []string,
	turnAt []int64,
) bool {
	n := len(turnFPs)
	if n < StuckNoProgressWindow {
		return false
	}

	// Build the set of file fingerprints seen in turns BEFORE the window.
	windowStart := n - StuckNoProgressWindow
	priorFPs := map[string]struct{}{}
	for i := 0; i < windowStart; i++ {
		priorFPs[turnFPs[i]] = struct{}{}
	}

	// Check whether any turn in the window introduced a new fingerprint.
	for i := windowStart; i < n; i++ {
		if _, seen := priorFPs[turnFPs[i]]; !seen {
			return false // frontier grew — not stuck
		}
	}

	// Check whether a validation_green appeared after the window start timestamp.
	windowStartAt := turnAt[windowStart]
	for _, e := range all {
		if e.Type == contract.LedgerEventValidationGreen && e.At >= windowStartAt {
			return false // a gate confirmed progress — not stuck
		}
	}

	return true
}
