// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jazz1x/rallish/pkg/contract"
)

// StateFileSync persists CycleState to a JSON file atomically.
// It is the adapter that bridges the in-memory domain to the filesystem.
//
// All instances sharing a path must be the same *StateFileSync so that mu
// serialises concurrent writers (e.g. an orchestrate goroutine racing a
// step/halt HTTP handler on the same cycle); two independent instances on the
// same path would not serialise, so callers reuse one syncer per path.
type StateFileSync struct {
	mu   sync.Mutex
	path string
}

// NewStateFileSync creates a syncer for the given file path.
func NewStateFileSync(path string) *StateFileSync {
	return &StateFileSync{path: path}
}

// Read deserialises the current state from disk.
// If the file does not exist, it returns a zero State (not an error).
// If the primary file is corrupt, it attempts to read the .bak backup.
func (s *StateFileSync) Read() (State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read state file: %w", err)
	}
	var cs contract.CycleState
	if err := json.Unmarshal(data, &cs); err != nil {
		// Primary file is corrupt; attempt backup recovery.
		bakData, bakErr := os.ReadFile(s.path + ".bak")
		if bakErr != nil {
			return State{}, fmt.Errorf("unmarshal state (corrupt) and no backup: %w", err)
		}
		if bakErr := json.Unmarshal(bakData, &cs); bakErr != nil {
			return State{}, fmt.Errorf("unmarshal state (corrupt) and backup also corrupt: original=%w, backup=%w", err, bakErr)
		}
	}
	return State{CycleState: cs}, nil
}

// Write serialises the state to disk atomically (write to a unique temp file,
// then a single rename). The rename never leaves the primary file absent, so a
// concurrent Read() always sees either the old or the new state — never a
// missing file that Read() would silently treat as a zero State.
//
// It preserves the prior primary content as a .bak backup, written *after* the
// new primary is in place (and only when there was prior content), so the .bak
// holds the previous good snapshot for crash recovery.
//
// mu serialises concurrent writers on the same path so two renames cannot race.
func (s *StateFileSync) Write(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(state.CycleState, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Snapshot the prior primary content (if any) to roll into .bak afterwards.
	// A missing primary is expected on first write and is not an error.
	prior, priorErr := os.ReadFile(s.path)
	if priorErr != nil && !os.IsNotExist(priorErr) {
		return fmt.Errorf("read prior state: %w", priorErr)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("fsync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	// Single atomic rename: the primary is never absent for a concurrent reader.
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename state: %w", err)
	}

	// Preserve the prior snapshot as .bak now that the new primary is committed.
	if priorErr == nil {
		//nolint:gosec // .bak derives from the same caller-configured s.path as the primary write
		if err := os.WriteFile(s.path+".bak", prior, 0o600); err != nil {
			return fmt.Errorf("write backup state: %w", err)
		}
	}
	return nil
}

// Path returns the configured file path.
func (s *StateFileSync) Path() string { return s.path }

// Remove deletes the state file (idempotent).
func (s *StateFileSync) Remove() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state file: %w", err)
	}
	return nil
}
