package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// exitCodeOf extracts the carried process exit code from an error, or -1 if the
// error does not carry one. Mirrors the errors.As branch in cmd/rallish/main.go.
func exitCodeOf(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return -1
}

// TestExitCodeForToolUseVerdictSSOT is the single-source-of-truth guard: every
// verdict maps to a documented code, and an unknown verdict fails safe (blocking).
func TestExitCodeForToolUseVerdictSSOT(t *testing.T) {
	cases := map[contract.ActionVerdict]int{
		contract.ActionAllow:     exitToolUseAllow,
		contract.ActionDeny:      exitToolUseDeny,
		contract.ActionNeedsHITL: exitToolUseNeedsHITL,
	}
	for v, want := range cases {
		if got := exitCodeForToolUseVerdict(v); got != want {
			t.Fatalf("exitCodeForToolUseVerdict(%q) = %d, want %d", v, got, want)
		}
	}
	if got := exitCodeForToolUseVerdict(contract.ActionVerdict("garbage")); got != exitToolUseDeny {
		t.Fatalf("unknown verdict must fail safe to deny code %d; got %d", exitToolUseDeny, got)
	}
}

// TestGateTooluseAllowSafeCommand is the FALSE-POSITIVE GUARD at the CLI layer: a
// legitimate command returns exit 0 (nil error), prints an allow decision, and is
// NOT recorded to the ledger even when a cycle context is supplied.
func TestGateTooluseAllowSafeCommand(t *testing.T) {
	recorded := 0
	opts := gateTooluseOptions{
		command:  "go test ./...",
		cycleID:  "cyc-1",
		stateDir: t.TempDir(),
		recordLedger: func(_ gateTooluseOptions, _ contract.HarnessLedgerEntry) error {
			recorded++
			return nil
		},
	}
	var out bytes.Buffer
	err := runGateTooluse(opts, &out)
	if err != nil {
		t.Fatalf("safe command must return nil (exit 0); got %v", err)
	}
	if recorded != 0 {
		t.Fatalf("safe command must NOT be recorded; recordLedger called %d times", recorded)
	}

	var decision contract.ToolUseDecision
	if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
		t.Fatalf("output must be JSON: %v (raw=%q)", err, out.String())
	}
	if decision.Verdict != contract.ActionAllow {
		t.Fatalf("verdict = %q, want allow", decision.Verdict)
	}
}

// TestGateTooluseDenyRecordsAndExits is the destructive-command path: a deny
// returns the exit-13 carrier, prints the deny decision, and records it.
func TestGateTooluseDenyRecordsAndExits(t *testing.T) {
	var captured contract.HarnessLedgerEntry
	recorded := 0
	opts := gateTooluseOptions{
		command:  "rm -rf /",
		cycleID:  "cyc-1",
		stateDir: t.TempDir(),
		recordLedger: func(_ gateTooluseOptions, e contract.HarnessLedgerEntry) error {
			recorded++
			captured = e
			return nil
		},
	}
	var out bytes.Buffer
	err := runGateTooluse(opts, &out)

	if code := exitCodeOf(err); code != exitToolUseDeny {
		t.Fatalf("deny exit code = %d, want %d (err=%v)", code, exitToolUseDeny, err)
	}
	if recorded != 1 {
		t.Fatalf("deny must be recorded exactly once; got %d", recorded)
	}
	if captured.Type != contract.LedgerEventToolUseDecision {
		t.Fatalf("recorded entry type = %q, want %q", captured.Type, contract.LedgerEventToolUseDecision)
	}
	if !strings.Contains(captured.Summary, "rm-rf-root") {
		t.Fatalf("recorded summary missing matched rule; got %q", captured.Summary)
	}

	var decision contract.ToolUseDecision
	if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
		t.Fatalf("output must be JSON: %v", err)
	}
	if decision.Verdict != contract.ActionDeny || decision.Check != contract.ToolUseCheckAction {
		t.Fatalf("decision = %+v, want deny via action check", decision)
	}
}

// TestGateTooluseNeedsHITLExitCode confirms a risky-but-maybe-legit command maps
// to the distinct needs-hitl exit code and is recorded.
func TestGateTooluseNeedsHITLExitCode(t *testing.T) {
	recorded := 0
	opts := gateTooluseOptions{
		command:  "cat ~/.aws/credentials",
		cycleID:  "cyc-1",
		stateDir: t.TempDir(),
		recordLedger: func(_ gateTooluseOptions, _ contract.HarnessLedgerEntry) error {
			recorded++
			return nil
		},
	}
	var out bytes.Buffer
	err := runGateTooluse(opts, &out)

	if code := exitCodeOf(err); code != exitToolUseNeedsHITL {
		t.Fatalf("needs-hitl exit code = %d, want %d (err=%v)", code, exitToolUseNeedsHITL, err)
	}
	if recorded != 1 {
		t.Fatalf("needs-hitl must be recorded once; got %d", recorded)
	}
}

// TestGateTooluseNoCycleNoRecord confirms that without --cycle-id a blocking
// decision still exits with the verdict code but is not recorded (no ledger
// target). The decision is always printed.
func TestGateTooluseNoCycleNoRecord(t *testing.T) {
	recorded := 0
	opts := gateTooluseOptions{
		command: "rm -rf /",
		recordLedger: func(_ gateTooluseOptions, _ contract.HarnessLedgerEntry) error {
			recorded++
			return nil
		},
	}
	var out bytes.Buffer
	err := runGateTooluse(opts, &out)

	if code := exitCodeOf(err); code != exitToolUseDeny {
		t.Fatalf("deny exit code = %d, want %d", code, exitToolUseDeny)
	}
	if recorded != 0 {
		t.Fatalf("no --cycle-id must mean no record; got %d", recorded)
	}
	if out.Len() == 0 {
		t.Fatal("decision must always be printed")
	}
}

// TestGateTooluseRecordsToOnDiskLedger exercises the DEFAULT (non-injected) record
// path end-to-end: a deny is appended to the cycle's real JSONL ledger, hash-
// chained, and reads back as a verified tooluse_decision entry.
func TestGateTooluseRecordsToOnDiskLedger(t *testing.T) {
	dir := t.TempDir()
	opts := gateTooluseOptions{
		command:  "git push --force origin main",
		cycleID:  "cyc-disk",
		stateDir: dir,
		// recordLedger nil ⇒ default appendToolUseLedgerEntry (real on-disk write).
	}
	var out bytes.Buffer
	err := runGateTooluse(opts, &out)
	if code := exitCodeOf(err); code != exitToolUseDeny {
		t.Fatalf("deny exit code = %d, want %d (err=%v)", code, exitToolUseDeny, err)
	}

	ledger := cycle.NewLedgerFileSync(dir + "/cycle-cyc-disk-ledger.jsonl")
	entries, rerr := ledger.ReadAll()
	if rerr != nil {
		t.Fatalf("read ledger: %v", rerr)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 ledger entry, got %d", len(entries))
	}
	if entries[0].Type != contract.LedgerEventToolUseDecision {
		t.Fatalf("entry type = %q, want %q", entries[0].Type, contract.LedgerEventToolUseDecision)
	}
	// The default path goes through LedgerFileSync.Append, which hash-chains on
	// write; the chain must verify (G4 tamper-evidence holds for this record).
	if idx, ok := contract.VerifyChain(entries); !ok {
		t.Fatalf("recorded ledger entry breaks the hash chain at index %d", idx)
	}
}
