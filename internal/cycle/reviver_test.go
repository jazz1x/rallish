package cycle

import (
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

func haltEntry(at int64, reason contract.HaltReason) contract.HarnessLedgerEntry {
	return contract.NewHarnessLedgerEntry(at, "cyc_r", contract.LedgerEventCycleHalted, string(reason), nil)
}

func greenEntry(at int64) contract.HarnessLedgerEntry {
	return contract.NewHarnessLedgerEntry(at, "cyc_r", contract.LedgerEventValidationGreen, "tests green", nil)
}

func turnEntry(at int64) contract.HarnessLedgerEntry {
	return contract.NewAgentTurnLedgerEntry(at, "cyc_r", "builder", contract.TurnResponse{Summary: "work"})
}

func TestLedgerSealsResume(t *testing.T) {
	tests := []struct {
		name       string
		entries    []contract.HarnessLedgerEntry
		wantSealed bool
		wantReason contract.HaltReason
	}{
		{
			name:       "empty ledger does not seal",
			entries:    nil,
			wantSealed: false,
		},
		{
			name:       "no halt does not seal",
			entries:    []contract.HarnessLedgerEntry{turnEntry(1), greenEntry(2)},
			wantSealed: false,
		},
		{
			name:       "halt with no later green seals",
			entries:    []contract.HarnessLedgerEntry{turnEntry(1), haltEntry(2, contract.HaltStuck)},
			wantSealed: true,
			wantReason: contract.HaltStuck,
		},
		{
			name:       "halt preserves its recorded reason",
			entries:    []contract.HarnessLedgerEntry{haltEntry(1, contract.HaltGateFailure)},
			wantSealed: true,
			wantReason: contract.HaltGateFailure,
		},
		{
			name:       "green after halt permits resume",
			entries:    []contract.HarnessLedgerEntry{haltEntry(1, contract.HaltStuck), greenEntry(2)},
			wantSealed: false,
		},
		{
			name:       "green before halt is stale and still seals",
			entries:    []contract.HarnessLedgerEntry{greenEntry(1), haltEntry(2, contract.HaltStuck)},
			wantSealed: true,
			wantReason: contract.HaltStuck,
		},
		{
			name: "second halt re-seals after an earlier permitted revive",
			entries: []contract.HarnessLedgerEntry{
				haltEntry(1, contract.HaltStuck),
				greenEntry(2),
				haltEntry(3, contract.HaltGateFailure),
			},
			wantSealed: true,
			wantReason: contract.HaltGateFailure,
		},
		{
			name:       "unparseable halt summary degrades to stuck",
			entries:    []contract.HarnessLedgerEntry{contract.NewHarnessLedgerEntry(1, "cyc_r", contract.LedgerEventCycleHalted, "", nil)},
			wantSealed: true,
			wantReason: contract.HaltStuck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, sealed := LedgerSealsResume(tt.entries)
			if sealed != tt.wantSealed {
				t.Fatalf("sealed = %v, want %v", sealed, tt.wantSealed)
			}
			if sealed && reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}
