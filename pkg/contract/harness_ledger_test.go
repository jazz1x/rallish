package contract

import "testing"

func TestChainHashIsDeterministic(t *testing.T) {
	entry := NewHarnessLedgerEntry(123, "cyc_1", LedgerEventCycleCreated, "created", []string{"a.go", "b.go"})
	prev := LedgerGenesisHash

	first, err := ChainHash(entry, prev)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ChainHash(entry, prev)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Fatalf("non-deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("hash len = %d, want 64 hex chars", len(first))
	}
}

func TestChainHashExcludesOwnHash(t *testing.T) {
	// The entry's own Hash field must not feed its hash, else the chain is
	// circular. Setting Hash to anything must not change the computed value.
	entry := NewHarnessLedgerEntry(1, "cyc_1", LedgerEventAgentTurn, "turn", nil)

	clean, err := ChainHash(entry, LedgerGenesisHash)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	entry.Hash = "deadbeef"
	withHash, err := ChainHash(entry, LedgerGenesisHash)
	if err != nil {
		t.Fatalf("withHash: %v", err)
	}
	if clean != withHash {
		t.Fatalf("hash field leaked into its own digest: %q != %q", clean, withHash)
	}
}

// buildChain constructs an n-entry valid hash chain exactly as LedgerFileSync
// .Append would (PrevHash = predecessor Hash, genesis for the first), so the
// verifier tests stay pure and in-package (no internal/cycle import).
func buildChain(t *testing.T, n int) []HarnessLedgerEntry {
	t.Helper()
	entries := make([]HarnessLedgerEntry, 0, n)
	prev := LedgerGenesisHash
	for i := 0; i < n; i++ {
		entry := NewHarnessLedgerEntry(int64(i+1), "cyc_1", LedgerEventAgentTurn, "turn", []string{"x.go"})
		entry.PrevHash = prev
		hash, err := ChainHash(entry, prev)
		if err != nil {
			t.Fatalf("chain hash %d: %v", i, err)
		}
		entry.Hash = hash
		entries = append(entries, entry)
		prev = hash
	}
	return entries
}

func TestVerifyChainIntact(t *testing.T) {
	idx, ok := VerifyChain(buildChain(t, 4))
	if !ok || idx != NoBrokenLink {
		t.Fatalf("intact chain: got (idx=%d, ok=%v), want (%d, true)", idx, ok, NoBrokenLink)
	}
}

func TestVerifyChainEmpty(t *testing.T) {
	idx, ok := VerifyChain(nil)
	if !ok || idx != NoBrokenLink {
		t.Fatalf("empty chain: got (idx=%d, ok=%v), want (%d, true)", idx, ok, NoBrokenLink)
	}
}

func TestVerifyChainTamperedMiddleEntry(t *testing.T) {
	entries := buildChain(t, 5)
	// Tamper with a MIDDLE entry's Summary after the fact, leaving its stored
	// Hash untouched. The recomputation no longer matches -> detected at index 2.
	entries[2].Summary = "tampered after the fact"

	idx, ok := VerifyChain(entries)
	if ok {
		t.Fatal("tampered chain reported intact")
	}
	if idx != 2 {
		t.Fatalf("broken index = %d, want 2 (the tampered entry)", idx)
	}
}

func TestVerifyChainBrokenLinkage(t *testing.T) {
	entries := buildChain(t, 4)
	// Re-point a middle entry's PrevHash so it no longer links to its
	// predecessor, while its own Hash stays self-consistent only for the old
	// link. The linkage check must flag the first divergence at index 3.
	entries[3].PrevHash = LedgerGenesisHash

	idx, ok := VerifyChain(entries)
	if ok {
		t.Fatal("relinked chain reported intact")
	}
	if idx != 3 {
		t.Fatalf("broken index = %d, want 3 (the relinked entry)", idx)
	}
}

func TestVerifyChainFirstEntryNonGenesis(t *testing.T) {
	entries := buildChain(t, 2)
	// A first entry whose PrevHash is not the genesis constant is an invalid
	// chain head, detected at index 0.
	entries[0].PrevHash = "1111111111111111111111111111111111111111111111111111111111111111"

	idx, ok := VerifyChain(entries)
	if ok {
		t.Fatal("bad genesis reported intact")
	}
	if idx != 0 {
		t.Fatalf("broken index = %d, want 0 (bad genesis head)", idx)
	}
}

func TestChainHashBindsPrevAndSchema(t *testing.T) {
	entry := NewHarnessLedgerEntry(1, "cyc_1", LedgerEventGatePassed, "ok", nil)

	base, err := ChainHash(entry, LedgerGenesisHash)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	// A different predecessor link yields a different hash (chain binding).
	diffPrev, err := ChainHash(entry, "1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("diffPrev: %v", err)
	}
	if base == diffPrev {
		t.Fatal("hash ignored prev_hash; chain not bound")
	}
	// A schema-version change is detectable (it is part of the canonical content).
	entry.SchemaVersion = "999"
	diffSchema, err := ChainHash(entry, LedgerGenesisHash)
	if err != nil {
		t.Fatalf("diffSchema: %v", err)
	}
	if base == diffSchema {
		t.Fatal("hash ignored schema_version; shape change undetectable")
	}
}

func TestNewHarnessLedgerEntryCopiesFiles(t *testing.T) {
	files := []string{"a.go", "b.go"}
	entry := NewHarnessLedgerEntry(123, "cyc_1", LedgerEventGatePassed, "tests passed", files)

	if entry.At != 123 {
		t.Fatalf("at = %d, want 123", entry.At)
	}
	if entry.SchemaVersion != LedgerSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", entry.SchemaVersion, LedgerSchemaVersion)
	}
	if entry.CycleID != "cyc_1" {
		t.Fatalf("cycle_id = %q, want cyc_1", entry.CycleID)
	}
	if entry.Type != LedgerEventGatePassed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGatePassed)
	}
	if entry.Summary != "tests passed" {
		t.Fatalf("summary = %q", entry.Summary)
	}
	if len(entry.Files) != 2 || entry.Files[0] != "a.go" || entry.Files[1] != "b.go" {
		t.Fatalf("files = %v", entry.Files)
	}

	files[0] = "mutated.go"
	if entry.Files[0] == "mutated.go" {
		t.Fatal("entry should copy files")
	}
}

func TestNewGateLedgerEntryPassed(t *testing.T) {
	report := GateReport{Gate: "audit", Passed: true, Stdout: "ok"}

	entry := NewGateLedgerEntry(456, "cyc_2", report)
	if entry.Type != LedgerEventGatePassed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGatePassed)
	}
	if entry.Gate != "audit" {
		t.Fatalf("gate = %q, want audit", entry.Gate)
	}
	if entry.Summary != "ok" {
		t.Fatalf("summary = %q, want ok", entry.Summary)
	}
}

func TestNewGateLedgerEntryFailed(t *testing.T) {
	report := GateReport{Gate: "audit", Passed: false, Stderr: "failed"}

	entry := NewGateLedgerEntry(789, "cyc_3", report)
	if entry.Type != LedgerEventGateFailed {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventGateFailed)
	}
	if entry.Gate != "audit" {
		t.Fatalf("gate = %q, want audit", entry.Gate)
	}
	if entry.Summary != "failed" {
		t.Fatalf("summary = %q, want failed", entry.Summary)
	}
}

func TestNewAgentTurnLedgerEntry(t *testing.T) {
	artifacts := []string{"agent.md"}
	resp := TurnResponse{
		Summary:   "implemented turn",
		Artifacts: artifacts,
	}

	entry := NewAgentTurnLedgerEntry(876, "cyc_5", "builder", resp)
	if entry.Type != LedgerEventAgentTurn {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventAgentTurn)
	}
	if entry.Agent != "builder" {
		t.Fatalf("agent = %q, want builder", entry.Agent)
	}
	if entry.Summary != "implemented turn" {
		t.Fatalf("summary = %q, want implemented turn", entry.Summary)
	}
	if len(entry.Files) != 1 || entry.Files[0] != "agent.md" {
		t.Fatalf("files = %v", entry.Files)
	}

	artifacts[0] = "mutated.md"
	if entry.Files[0] == "mutated.md" {
		t.Fatal("entry should copy artifacts")
	}
}

func TestNewHandoffLedgerEntry(t *testing.T) {
	artifacts := []string{"handoff.md"}
	resp := TurnResponse{
		HandoffTo: "reviewer",
		Summary:   "ready for review",
		Artifacts: artifacts,
	}

	entry := NewHandoffLedgerEntry(987, "cyc_4", "builder", resp)
	if entry.Type != LedgerEventHandoffCreated {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventHandoffCreated)
	}
	if entry.Agent != "builder" {
		t.Fatalf("agent = %q, want builder", entry.Agent)
	}
	if entry.HandoffTo != "reviewer" {
		t.Fatalf("handoff_to = %q, want reviewer", entry.HandoffTo)
	}
	if entry.Summary != "ready for review" {
		t.Fatalf("summary = %q, want ready for review", entry.Summary)
	}
	if len(entry.Files) != 1 || entry.Files[0] != "handoff.md" {
		t.Fatalf("files = %v", entry.Files)
	}

	artifacts[0] = "mutated.md"
	if entry.Files[0] == "mutated.md" {
		t.Fatal("entry should copy artifacts")
	}
}
