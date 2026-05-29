package cycle

// reviver.go — sticky-halt guard over the harness ledger (G5 reviver guard).
//
// A pure harness can only halt; it cannot stop a cron/driver from re-triggering
// orchestration for the same cycle. Halt already removes the mutable state file
// ("zombie prevention"), but the append-only ledger persists at
// tmp/cycle-<id>-ledger.jsonl. This guard reads that ledger so a re-trigger
// cannot silently revive a run that has already halted.
//
// Rule: a cycle_halted entry is STICKY. Resume is only permitted if measurable
// progress was recorded AFTER the halt — a validation_green event (the only
// un-gameable progress signal, produced by the verifier) committed later. No
// such event ⇒ the halt seals the cycle and a re-trigger must be refused.
//
// Like stuck.go these are cheap O(n) equality/ordering checks over the ledger,
// not subgraph isomorphism.

import "github.com/jazz1x/rallish/pkg/contract"

// LedgerSealsResume reports whether the ledger forbids (re)starting orchestration
// for this cycle. It returns true together with the sticky HaltReason when the
// last terminal cycle_halted entry has no later validation_green; it returns
// ("", false) when there is no halt, or when a validation_green recorded after
// the most recent halt shows measurable progress (a permitted revive).
//
// It is a pure function: no I/O, O(n) in len(entries), single pass.
func LedgerSealsResume(entries []contract.HarnessLedgerEntry) (contract.HaltReason, bool) {
	// Walk once, tracking the most recent halt (reason + position) and whether a
	// validation_green appeared at or after that halt position. Position (index)
	// is the ordering key rather than the At timestamp so a coarse or duplicated
	// clock cannot defeat the seal.
	lastHaltIdx := -1
	var lastHaltReason contract.HaltReason
	greenAfterHalt := false

	for i, e := range entries {
		switch e.Type {
		case contract.LedgerEventCycleHalted:
			lastHaltIdx = i
			lastHaltReason = parseHaltReasonOrStuck(e.Summary)
			greenAfterHalt = false // a fresh halt re-seals; earlier progress is stale
		case contract.LedgerEventValidationGreen:
			if lastHaltIdx >= 0 {
				greenAfterHalt = true
			}
		}
	}

	if lastHaltIdx < 0 {
		return "", false // never halted — resume freely
	}
	if greenAfterHalt {
		return "", false // measurable progress after the halt — revive permitted
	}
	return lastHaltReason, true
}

// parseHaltReasonOrStuck recovers the HaltReason recorded in a cycle_halted
// entry's Summary (appendLedger / orchestrator write it as string(reason)).
// An unparseable or empty summary degrades to HaltStuck so the seal still holds
// with a known reason rather than an empty one.
func parseHaltReasonOrStuck(summary string) contract.HaltReason {
	if reason, err := contract.ParseHaltReason(summary); err == nil {
		return reason
	}
	return contract.HaltStuck
}
