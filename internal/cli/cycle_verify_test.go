package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

// buildLedger appends n agent_turn entries through the real LedgerFileSync (which
// computes prev_hash/hash), then reads them back — a genuine hash-chained log.
func buildLedger(t *testing.T, n int) []contract.HarnessLedgerEntry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	led := cycle.NewLedgerFileSync(path)
	for i := 0; i < n; i++ {
		e := contract.NewHarnessLedgerEntry(int64(i+1), "cyc-test", contract.LedgerEventAgentTurn,
			"turn summary", nil)
		require.NoError(t, led.Append(e))
	}
	entries, err := led.ReadAll()
	require.NoError(t, err)
	require.Len(t, entries, n)
	return entries
}

func TestVerifyLedgerEntries_IntactChainAndProofs(t *testing.T) {
	entries := buildLedger(t, 4)
	var buf bytes.Buffer
	opts := cycleVerifyOptions{cycleID: "cyc-test", inclusion: 1, consistency: 2}

	err := verifyLedgerEntries(entries, opts, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "hash-chain:   ✓ intact")
	require.Contains(t, out, "merkle-root:")
	require.Contains(t, out, "inclusion[1]: ✓ committed")
	require.Contains(t, out, "consistency:  ✓")
}

func TestVerifyLedgerEntries_TamperedChainFails(t *testing.T) {
	entries := buildLedger(t, 3)
	entries[1].Summary = "mutated after hashing" // breaks the recomputed content hash

	var buf bytes.Buffer
	err := verifyLedgerEntries(entries, cycleVerifyOptions{cycleID: "cyc-test", inclusion: -1, consistency: -1}, &buf)

	require.Error(t, err)
	var ve *verifyFailedError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, 15, ve.ExitCode())
	require.Contains(t, buf.String(), "✗ TAMPERED at entry 1")
}

func TestVerifyLedgerEntries_OutOfRangeInclusionFails(t *testing.T) {
	entries := buildLedger(t, 2)
	var buf bytes.Buffer
	err := verifyLedgerEntries(entries, cycleVerifyOptions{cycleID: "cyc-test", inclusion: 9, consistency: -1}, &buf)
	require.Error(t, err)
	require.Contains(t, buf.String(), "inclusion[9]: ✗")
}

func TestVerifyLedgerEntries_ConsistencyOverSizeFails(t *testing.T) {
	entries := buildLedger(t, 2)
	var buf bytes.Buffer
	err := verifyLedgerEntries(entries, cycleVerifyOptions{cycleID: "cyc-test", inclusion: -1, consistency: 5}, &buf)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(buf.String()), "exceeds")
}
