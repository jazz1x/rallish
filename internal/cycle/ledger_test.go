package cycle

import (
	"path/filepath"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

func TestLedgerFileSyncAppendReadAll(t *testing.T) {
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "cycle-ledger.jsonl"))

	first := contract.NewHarnessLedgerEntry(1, "cyc_1", contract.LedgerEventCycleCreated, "created", []string{"a.go"})
	second := contract.NewHarnessLedgerEntry(2, "cyc_1", contract.LedgerEventValidationGreen, "green", []string{"b.go"})
	if err := ledger.Append(first); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := ledger.Append(second); err != nil {
		t.Fatalf("append second: %v", err)
	}

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Type != contract.LedgerEventCycleCreated {
		t.Fatalf("first type = %q", entries[0].Type)
	}
	if entries[1].Type != contract.LedgerEventValidationGreen {
		t.Fatalf("second type = %q", entries[1].Type)
	}
	if entries[0].Files[0] != "a.go" || entries[1].Files[0] != "b.go" {
		t.Fatalf("files = %#v", entries)
	}
}

func TestLedgerFileSyncAppendChainsHashes(t *testing.T) {
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "cycle-ledger.jsonl"))

	for i, typ := range []contract.LedgerEventType{
		contract.LedgerEventCycleCreated,
		contract.LedgerEventAgentTurn,
		contract.LedgerEventValidationGreen,
	} {
		entry := contract.NewHarnessLedgerEntry(int64(i+1), "cyc_1", typ, "step", []string{"x.go"})
		if err := ledger.Append(entry); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}

	// Genesis: the first entry's PrevHash is the all-zero genesis constant.
	if entries[0].PrevHash != contract.LedgerGenesisHash {
		t.Fatalf("genesis prev_hash = %q, want %q", entries[0].PrevHash, contract.LedgerGenesisHash)
	}
	// Every entry carries a non-empty hash and chains to its predecessor.
	for i, entry := range entries {
		if entry.Hash == "" {
			t.Fatalf("entry %d has empty hash", i)
		}
		if i > 0 && entry.PrevHash != entries[i-1].Hash {
			t.Fatalf("entry %d prev_hash = %q, want prior hash %q", i, entry.PrevHash, entries[i-1].Hash)
		}
		// The stored hash matches a recomputation over the round-tripped entry,
		// proving the canonical form survives marshal/unmarshal.
		want, herr := contract.ChainHash(entry, entry.PrevHash)
		if herr != nil {
			t.Fatalf("recompute %d: %v", i, herr)
		}
		if entry.Hash != want {
			t.Fatalf("entry %d hash = %q, recompute = %q", i, entry.Hash, want)
		}
	}
}

func TestLedgerFileSyncReadAllMissingFile(t *testing.T) {
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "missing.jsonl"))

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}
