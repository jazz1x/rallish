// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jazz1x/rallish/pkg/contract"
)

// StateFileSync persists CycleState to a JSON file atomically.
// It is the adapter that bridges the in-memory domain to the filesystem.
type StateFileSync struct {
	path string
}

// NewStateFileSync creates a syncer for the given file path.
func NewStateFileSync(path string) *StateFileSync {
	return &StateFileSync{path: path}
}

// Read deserialises the current state from disk.
// If the file does not exist, it returns a zero State (not an error).
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
		return State{}, fmt.Errorf("unmarshal state: %w", err)
	}
	return State{CycleState: cs}, nil
}

// Write serialises the state to disk atomically (write to temp, then rename).
func (s *StateFileSync) Write(state State) error {
	data, err := json.MarshalIndent(state.CycleState, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename state: %w", err)
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
