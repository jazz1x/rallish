package cycle

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

// TestLedgerFileSyncConcurrentAppendKeepsChainIntact is the regression guard for
// the concurrent-append fork: many goroutines appending to the SAME cycle ledger
// (via DISTINCT LedgerFileSync instances, as the broker does on every
// appendLedger) must serialize so the hash chain never forks. Run with -race.
func TestLedgerFileSyncConcurrentAppendKeepsChainIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cyc-concurrent-ledger.jsonl")

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// A fresh instance per call — mirrors broker.appendLedger, which an
			// instance-level mutex could not serialize.
			ledger := NewLedgerFileSync(path)
			entry := contract.NewHarnessLedgerEntry(int64(i), "cyc_conc", contract.LedgerEventAgentTurn, "turn", nil)
			if err := ledger.Append(entry); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	entries, err := NewLedgerFileSync(path).ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("entries = %d, want %d (a lost write means the lock did not serialize)", len(entries), n)
	}
	// The chain must verify end to end: no fork, every PrevHash links the tail.
	if broken, ok := contract.VerifyChain(entries); !ok {
		t.Fatalf("VerifyChain failed at index %d — concurrent appends forked the chain", broken)
	}
}

// TestLedgerFileSyncAppendResumesExistingChain guards that a fresh LedgerFileSync
// instance appending to an existing ledger reads the tail hash once and continues
// the chain without forking (the cache initialization path).
func TestLedgerFileSyncAppendResumesExistingChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cyc-resume.jsonl")
	ledger := NewLedgerFileSync(path)
	first := contract.NewHarnessLedgerEntry(1, "cyc_1", contract.LedgerEventCycleCreated, "created", nil)
	if err := ledger.Append(first); err != nil {
		t.Fatalf("append first: %v", err)
	}

	// A new instance simulates daemon revival or a separate caller: it must read
	// the existing tail hash and link the next entry to it.
	ledger2 := NewLedgerFileSync(path)
	second := contract.NewHarnessLedgerEntry(2, "cyc_1", contract.LedgerEventAgentTurn, "turn", nil)
	if err := ledger2.Append(second); err != nil {
		t.Fatalf("append second via new instance: %v", err)
	}

	entries, err := NewLedgerFileSync(path).ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[1].PrevHash != entries[0].Hash {
		t.Fatalf("second prev_hash = %q, want first hash %q", entries[1].PrevHash, entries[0].Hash)
	}
}

// TestLedgerFileSyncHandlesOversizedEntry is the regression guard for the 64 KiB
// bufio.Scanner limit: an entry whose JSONL line exceeds 64 KiB must round-trip
// through Append/lastHash/ReadAll without bricking the chain. False-positive
// guard: a normal small entry appended after it still chains correctly.
func TestLedgerFileSyncHandlesOversizedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cyc-oversized-ledger.jsonl")
	ledger := NewLedgerFileSync(path)

	// ~200 KiB summary — well past bufio.Scanner's 64 KiB default token cap.
	big := strings.Repeat("x", 200*1024)
	if err := ledger.Append(contract.NewHarnessLedgerEntry(1, "cyc_big", contract.LedgerEventGateFailed, big, nil)); err != nil {
		t.Fatalf("append oversized: %v", err)
	}
	// A normal entry after the big one must still link (lastHash must have read
	// the oversized predecessor, not choked on it).
	if err := ledger.Append(contract.NewHarnessLedgerEntry(2, "cyc_big", contract.LedgerEventValidationGreen, "green", nil)); err != nil {
		t.Fatalf("append after oversized: %v", err)
	}

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (oversized line was dropped by a size cap)", len(entries))
	}
	if len(entries[0].Summary) != len(big) {
		t.Fatalf("oversized summary truncated: got %d bytes, want %d", len(entries[0].Summary), len(big))
	}
	if broken, ok := contract.VerifyChain(entries); !ok {
		t.Fatalf("VerifyChain failed at index %d after an oversized entry", broken)
	}
}

// BenchmarkLedgerAppend measures that ledger append cost (including the
// predecessor-hash lookup) does not grow with ledger size. The process-wide
// tail-hash cache makes this O(1) per append once the cache is warm.
func BenchmarkLedgerAppend(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "ledger.jsonl")
			ledger := NewLedgerFileSync(path)
			for i := 0; i < size; i++ {
				entry := contract.NewHarnessLedgerEntry(int64(i), "cyc_bench", contract.LedgerEventAgentTurn, "turn", nil)
				if err := ledger.Append(entry); err != nil {
					b.Fatalf("warmup append: %v", err)
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				entry := contract.NewHarnessLedgerEntry(int64(size+i), "cyc_bench", contract.LedgerEventAgentTurn, "turn", nil)
				if err := ledger.Append(entry); err != nil {
					b.Fatalf("append: %v", err)
				}
			}
		})
	}
}
