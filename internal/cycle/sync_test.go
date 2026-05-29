package cycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// TestStateFileSyncRecoversFromBak is the resume-after-limit substrate check
// (G1, north-star line 97): a session that dies mid-write can leave the primary
// state file truncated/corrupt, but the previous good copy survives as .bak.
// Read() MUST round-trip the .bak so a re-triggered run resumes from saved
// state. This complements TestStateFileSyncAtomicWriteAndRecovery in
// driver_test.go by asserting the .bak holds the exact prior snapshot.
func TestStateFileSyncRecoversFromBak(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: resume", MaxCycles: 7}, "cyc_resume_bak")
	state.CompletedCycles = 3
	if err := sync.Write(state); err != nil { // first write: the snapshot to recover
		t.Fatalf("write 1: %v", err)
	}
	state.CompletedCycles = 4
	if err := sync.Write(state); err != nil { // second write: rotates the prior into .bak
		t.Fatalf("write 2: %v", err)
	}

	// Simulate a limit-death that left the primary half-written.
	if err := os.WriteFile(tmp, []byte(`{"id":"cyc_resume_bak","comple`), 0o600); err != nil {
		t.Fatalf("truncate primary: %v", err)
	}

	recovered, err := sync.Read()
	if err != nil {
		t.Fatalf("read after truncate: %v", err)
	}
	if recovered.ID != "cyc_resume_bak" {
		t.Fatalf("recovered id = %q, want cyc_resume_bak", recovered.ID)
	}
	// .bak holds the *first* write (3), since the second write rotated it in.
	if recovered.CompletedCycles != 3 {
		t.Fatalf("recovered completed_cycles = %d, want 3 (the .bak snapshot)", recovered.CompletedCycles)
	}
}

// TestStateFileSyncCorruptPrimaryNoBackupErrors covers the missing edge of the
// .bak contract (sync.go corrupt-primary branch): a corrupt primary with no
// recoverable backup is an ERROR, never a silent zero State. ROP: an
// unrecoverable read surfaces, it is not papered over with empty state that
// would let a resume start from scratch and lose progress.
func TestStateFileSyncCorruptPrimaryNoBackupErrors(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle.json")
	sync := NewStateFileSync(tmp)

	// Corrupt primary, and never created a .bak.
	if err := os.WriteFile(tmp, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt primary: %v", err)
	}

	if _, err := sync.Read(); err == nil {
		t.Fatal("expected an error for corrupt primary with no backup, got nil")
	}

	// And when the .bak is *also* corrupt, the error must still surface.
	if err := os.WriteFile(tmp+".bak", []byte("also not json"), 0o600); err != nil {
		t.Fatalf("write corrupt bak: %v", err)
	}
	if _, err := sync.Read(); err == nil {
		t.Fatal("expected an error when both primary and backup are corrupt, got nil")
	}
}

// TestStateFileSyncMissingFileIsZeroState confirms the resume entry point for a
// brand-new cycle: a non-existent state file is NOT an error — it yields the
// zero State so a first run can begin (distinct from the corrupt-but-present
// case above, which is fatal).
func TestStateFileSyncMissingFileIsZeroState(t *testing.T) {
	sync := NewStateFileSync(filepath.Join(t.TempDir(), "absent.json"))
	state, err := sync.Read()
	if err != nil {
		t.Fatalf("read missing file: %v", err)
	}
	if state.ID != "" || state.CompletedCycles != 0 {
		t.Fatalf("missing file should yield zero State, got %#v", state.CycleState)
	}
}
