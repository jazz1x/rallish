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
