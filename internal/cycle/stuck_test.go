package cycle

import (
	"fmt"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// mkTurn builds a minimal agent-turn ledger entry at timestamp at.
func mkTurn(at int64, summary string, files []string) contract.HarnessLedgerEntry {
	return contract.HarnessLedgerEntry{
		SchemaVersion: contract.LedgerSchemaVersion,
		At:            at,
		Type:          contract.LedgerEventAgentTurn,
		Summary:       summary,
		Files:         files,
	}
}

// mkGateFailed builds a gate-failed ledger entry.
func mkGateFailed(at int64, gate, summary string) contract.HarnessLedgerEntry {
	return contract.HarnessLedgerEntry{
		SchemaVersion: contract.LedgerSchemaVersion,
		At:            at,
		Type:          contract.LedgerEventGateFailed,
		Gate:          gate,
		Summary:       summary,
	}
}

// mkValidationGreen builds a validation-green ledger entry.
func mkValidationGreen(at int64) contract.HarnessLedgerEntry {
	return contract.HarnessLedgerEntry{
		SchemaVersion: contract.LedgerSchemaVersion,
		At:            at,
		Type:          contract.LedgerEventValidationGreen,
	}
}

func TestStuck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		entries   []contract.HarnessLedgerEntry
		wantStuck bool
	}{
		// -------------------------------------------------------
		// empty input
		// -------------------------------------------------------
		{
			name:      "empty slice is not stuck",
			entries:   nil,
			wantStuck: false,
		},

		// -------------------------------------------------------
		// predicate a: repeated turn
		// -------------------------------------------------------
		{
			name: "a: repeated turn — stuck when same fingerprint >= threshold",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				for i := 0; i < StuckRepeatTurnThreshold; i++ {
					es = append(es, mkTurn(int64(i+1), "fix lint", []string{"main.go"}))
				}
				return es
			}(),
			wantStuck: true,
		},
		{
			name: "a: repeated turn — not stuck when count < threshold",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				for i := 0; i < StuckRepeatTurnThreshold-1; i++ {
					es = append(es, mkTurn(int64(i+1), "fix lint", []string{"main.go"}))
				}
				return es
			}(),
			wantStuck: false,
		},

		// -------------------------------------------------------
		// predicate b: repeated gate failure
		// -------------------------------------------------------
		{
			name: "b: repeated gate failure — stuck when same (gate,summary) >= threshold",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				for i := 0; i < StuckRepeatGateFailThreshold; i++ {
					es = append(es, mkGateFailed(int64(i+1), "lint", "golangci-lint failed"))
				}
				return es
			}(),
			wantStuck: true,
		},
		{
			name: "b: repeated gate failure — not stuck when count < threshold",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				for i := 0; i < StuckRepeatGateFailThreshold-1; i++ {
					es = append(es, mkGateFailed(int64(i+1), "lint", "golangci-lint failed"))
				}
				return es
			}(),
			wantStuck: false,
		},

		// -------------------------------------------------------
		// predicate c: ping-pong
		// -------------------------------------------------------
		{
			name: "c: ping-pong — stuck when A,B alternates >= threshold",
			entries: func() []contract.HarnessLedgerEntry {
				fps := []struct {
					s string
					f []string
				}{
					{"do A", []string{"a.go"}},
					{"do B", []string{"b.go"}},
				}
				var es []contract.HarnessLedgerEntry
				for i := 0; i < StuckPingPongThreshold; i++ {
					fp := fps[i%2]
					es = append(es, mkTurn(int64(i+1), fp.s, fp.f))
				}
				return es
			}(),
			wantStuck: true,
		},
		{
			name: "c: ping-pong — not stuck when alternating run < threshold",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				fps := []struct {
					s string
					f []string
				}{
					{"do A", []string{"a.go"}},
					{"do B", []string{"b.go"}},
				}
				for i := 0; i < StuckPingPongThreshold-1; i++ {
					fp := fps[i%2]
					es = append(es, mkTurn(int64(i+1), fp.s, fp.f))
				}
				return es
			}(),
			wantStuck: false,
		},

		// -------------------------------------------------------
		// predicate d: no progress
		// -------------------------------------------------------
		{
			// Prior turns each have a unique fingerprint (summary+file distinct per
			// index) so predicate-a never fires (each count=1 before the window).
			// The last StuckNoProgressWindow window turns each reuse a *different*
			// prior fingerprint (count becomes 2, below threshold=4).
			// No validation_green follows — predicate-d must fire.
			name: "d: no progress — stuck when window has no new files and no validation_green",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				// priorCount distinct fingerprints (one per index).
				priorCount := StuckNoProgressWindow + 1
				for i := 0; i < priorCount; i++ {
					es = append(es, mkTurn(int64(i+1), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				// Window: reuse the first StuckNoProgressWindow prior fingerprints
				// (each now appears exactly twice — below predicate-a threshold=4).
				for i := 0; i < StuckNoProgressWindow; i++ {
					es = append(es, mkTurn(int64(priorCount+i+1), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				return es
			}(),
			wantStuck: true,
		},
		{
			// Same setup as the positive case but a validation_green appears after
			// window start — predicate-d must NOT fire.
			name: "d: no progress — not stuck when validation_green appears inside window",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				priorCount := StuckNoProgressWindow + 1
				windowStartAt := int64(priorCount + 1)
				for i := 0; i < priorCount; i++ {
					es = append(es, mkTurn(int64(i+1), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				for i := 0; i < StuckNoProgressWindow; i++ {
					es = append(es, mkTurn(windowStartAt+int64(i), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				// validation_green timestamped >= window start.
				es = append(es, mkValidationGreen(windowStartAt+int64(StuckNoProgressWindow)+1))
				return es
			}(),
			wantStuck: false,
		},
		{
			// Same setup but the last entry in the window introduces a brand-new file
			// fingerprint — predicate-d must NOT fire.
			name: "d: no progress — not stuck when a new file fingerprint appears in window",
			entries: func() []contract.HarnessLedgerEntry {
				var es []contract.HarnessLedgerEntry
				priorCount := StuckNoProgressWindow + 1
				for i := 0; i < priorCount; i++ {
					es = append(es, mkTurn(int64(i+1), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				// Window: all-but-last reuse a prior fingerprint, last is brand new.
				for i := 0; i < StuckNoProgressWindow-1; i++ {
					es = append(es, mkTurn(int64(priorCount+i+1), fmt.Sprintf("s%d", i), []string{fmt.Sprintf("f%d.go", i)}))
				}
				es = append(es, mkTurn(int64(priorCount+StuckNoProgressWindow+1), "brand-new", []string{"brand_new.go"}))
				return es
			}(),
			wantStuck: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason, stuck := Stuck(tc.entries)
			if stuck != tc.wantStuck {
				t.Errorf("Stuck() stuck=%v, want %v", stuck, tc.wantStuck)
			}
			if stuck && reason != contract.HaltStuck {
				t.Errorf("Stuck() reason=%q, want %q", reason, contract.HaltStuck)
			}
			if !stuck && reason != "" {
				t.Errorf("Stuck() reason=%q when not stuck, want empty", reason)
			}
		})
	}
}
