package contract_test

import (
	"strings"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// TestPinGatesIdenticalRoundTrip asserts that two calls on the same input
// produce equal digests (determinism).
func TestPinGatesIdenticalRoundTrip(t *testing.T) {
	cmds := []string{"go test ./...", "golangci-lint run ./..."}
	d1, _ := contract.PinGates(cmds)
	d2, _ := contract.PinGates(cmds)
	if d1 != d2 {
		t.Fatalf("non-deterministic: %q != %q", d1, d2)
	}
	if len(d1) != 64 {
		t.Fatalf("digest len = %d, want 64 hex chars", len(d1))
	}
}

// TestPinGatesEmptySet asserts that an empty gate list yields a well-defined
// (non-empty) digest — absence-of-gates is auditable.
func TestPinGatesEmptySet(t *testing.T) {
	digest, pins := contract.PinGates(nil)
	if digest == "" {
		t.Fatal("empty gate set must still produce a digest")
	}
	if len(digest) != 64 {
		t.Fatalf("digest len = %d, want 64", len(digest))
	}
	if len(pins) != 0 {
		t.Fatalf("want 0 pins for empty set, got %d", len(pins))
	}
}

// TestPinGatesPinFields asserts that each GatePin carries the expected Name
// ("cmd:"+command) and a 64-char hex CommandHash.
func TestPinGatesPinFields(t *testing.T) {
	cmds := []string{"go build ./...", "go vet ./..."}
	_, pins := contract.PinGates(cmds)
	if len(pins) != 2 {
		t.Fatalf("want 2 pins, got %d", len(pins))
	}
	for i, p := range pins {
		wantName := "cmd:" + cmds[i]
		if p.Name != wantName {
			t.Errorf("pins[%d].Name = %q, want %q", i, p.Name, wantName)
		}
		if len(p.CommandHash) != 64 {
			t.Errorf("pins[%d].CommandHash len = %d, want 64", i, len(p.CommandHash))
		}
	}
}

// TestGatesTamperedNotTamperedFalsePositiveGuard is the false-positive guard:
// an unchanged gate set across a pin-then-check round-trip must NOT be flagged
// as tampered.
func TestGatesTamperedNotTamperedFalsePositiveGuard(t *testing.T) {
	cmds := []string{"go test ./...", "gofmt -l ."}
	digest, _ := contract.PinGates(cmds)
	if contract.GatesTampered(digest, cmds) {
		t.Fatal("identical gate set reported as tampered (false positive)")
	}
}

// TestGatesTamperedDetectsEdit asserts that editing a command is detected.
func TestGatesTamperedDetectsEdit(t *testing.T) {
	original := []string{"go test ./...", "golangci-lint run ./..."}
	digest, _ := contract.PinGates(original)

	edited := []string{"go test ./...", "golangci-lint run ./... --fix"} // command changed
	if !contract.GatesTampered(digest, edited) {
		t.Fatal("edited command not detected as tampered")
	}
}

// TestGatesTamperedDetectsRemoval asserts that removing a gate is detected.
func TestGatesTamperedDetectsRemoval(t *testing.T) {
	original := []string{"go test ./...", "golangci-lint run ./..."}
	digest, _ := contract.PinGates(original)

	reduced := []string{"go test ./..."} // one gate removed
	if !contract.GatesTampered(digest, reduced) {
		t.Fatal("gate removal not detected as tampered")
	}
}

// TestGatesTamperedDetectsReorder asserts that reordering gates is detected
// (order is part of the gate definition contract).
func TestGatesTamperedDetectsReorder(t *testing.T) {
	original := []string{"go test ./...", "golangci-lint run ./..."}
	digest, _ := contract.PinGates(original)

	reordered := []string{"golangci-lint run ./...", "go test ./..."} // swapped
	if !contract.GatesTampered(digest, reordered) {
		t.Fatal("gate reorder not detected as tampered")
	}
}

// TestGatesTamperedEmptySetIsStable asserts that an empty set pinned and then
// checked as empty is NOT flagged as tampered.
func TestGatesTamperedEmptySetIsStable(t *testing.T) {
	digest, _ := contract.PinGates(nil)
	if contract.GatesTampered(digest, nil) {
		t.Fatal("empty gate set flagged as tampered against itself (false positive)")
	}
	if contract.GatesTampered(digest, []string{}) {
		t.Fatal("empty slice flagged as tampered against nil-pinned digest (false positive)")
	}
}

// TestNewGatesPinnedEntryPinned validates the ledger entry for a normal pin event.
func TestNewGatesPinnedEntryPinned(t *testing.T) {
	cmds := []string{"go test ./...", "golangci-lint run ./..."}
	digest, pins := contract.PinGates(cmds)
	entry := contract.NewGatesPinnedEntry(1000, "cyc_1", digest, pins, false)

	if entry.Type != contract.LedgerEventGatesPinned {
		t.Fatalf("type = %q, want %q", entry.Type, contract.LedgerEventGatesPinned)
	}
	if entry.CycleID != "cyc_1" {
		t.Fatalf("cycle_id = %q, want cyc_1", entry.CycleID)
	}
	if !strings.Contains(entry.Summary, digest) {
		t.Fatalf("summary %q does not contain digest %q", entry.Summary, digest)
	}
	if !strings.Contains(entry.Summary, "gates pinned") {
		t.Fatalf("summary %q missing 'gates pinned' prefix", entry.Summary)
	}
	if len(entry.Files) != len(pins) {
		t.Fatalf("files len = %d, want %d (one per pin)", len(entry.Files), len(pins))
	}
}

// TestNewGatesPinnedEntryTampered validates the ledger entry for a tamper event.
func TestNewGatesPinnedEntryTampered(t *testing.T) {
	cmds := []string{"go test ./..."}
	digest, pins := contract.PinGates(cmds)
	entry := contract.NewGatesPinnedEntry(2000, "cyc_2", digest, pins, true)

	if entry.Type != contract.LedgerEventGateTampered {
		t.Fatalf("type = %q, want %q", entry.Type, contract.LedgerEventGateTampered)
	}
	if !strings.Contains(entry.Summary, "tampered") {
		t.Fatalf("summary %q missing 'tampered' signal", entry.Summary)
	}
}

// TestNewGatesPinnedEntryEmptyGates asserts that an empty gate set produces a
// valid entry with count=0 — absence-of-gates is auditable, not suppressed.
func TestNewGatesPinnedEntryEmptyGates(t *testing.T) {
	digest, pins := contract.PinGates(nil)
	entry := contract.NewGatesPinnedEntry(3000, "cyc_3", digest, pins, false)

	if entry.Type != contract.LedgerEventGatesPinned {
		t.Fatalf("type = %q, want %q", entry.Type, contract.LedgerEventGatesPinned)
	}
	if !strings.Contains(entry.Summary, "count=0") {
		t.Fatalf("summary %q missing 'count=0'", entry.Summary)
	}
	if len(entry.Files) != 0 {
		t.Fatalf("files = %v, want empty for zero gates", entry.Files)
	}
}

// TestPinGatesDifferentInputsDifferentDigests asserts that distinct inputs
// produce distinct digests (collision-resistance over the relevant input space).
func TestPinGatesDifferentInputsDifferentDigests(t *testing.T) {
	d1, _ := contract.PinGates([]string{"cmd-a"})
	d2, _ := contract.PinGates([]string{"cmd-b"})
	if d1 == d2 {
		t.Fatal("distinct gate sets produced the same digest")
	}
}
