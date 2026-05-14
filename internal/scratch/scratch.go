// Package scratch manages the rolling shared scratchpad and compaction triggers.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
)

// Manager owns a single scratchpad file on disk.
type Manager struct {
	path  string
	maxKB int64
}

// NewManager creates a Manager for the given file path.
func NewManager(path string, maxKB int64) *Manager {
	return &Manager{path: path, maxKB: maxKB}
}

// ForSession returns a Manager scoped to a session directory.
func ForSession(sessionDir string, maxKB int64) *Manager {
	return NewManager(filepath.Join(sessionDir, "scratch.md"), maxKB)
}

// Append adds content to the scratchpad and triggers compaction if over limit.
func (m *Manager) Append(content string) error {
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is controlled by broker
	if err != nil {
		return fmt.Errorf("open scratchpad: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := fmt.Fprint(f, content); err != nil {
		return fmt.Errorf("write scratchpad: %w", err)
	}
	return m.compactIfNeeded()
}

// Read returns the current scratchpad contents.
func (m *Manager) Read() (string, error) {
	b, err := os.ReadFile(m.path) //nolint:gosec // path is controlled by broker
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read scratchpad: %w", err)
	}
	return string(b), nil
}

// Path returns the scratchpad file path.
func (m *Manager) Path() string { return m.path }

func (m *Manager) compactIfNeeded() error {
	if m.maxKB <= 0 {
		return nil
	}
	info, err := os.Stat(m.path)
	if err != nil {
		return err
	}
	limit := m.maxKB * 1024
	// Trigger compaction at 80 % of the limit so we leave headroom for
	// future appends without immediately re-compacting.
	threshold := limit * 8 / 10
	if info.Size() <= threshold {
		return nil
	}

	data, err := os.ReadFile(m.path) //nolint:gosec // path is controlled by broker
	if err != nil {
		return err
	}

	summary := []byte("<!-- Scratchpad auto-compacted; older content truncated -->\n\n")

	// We need len(summary) + len(data) - half <= limit,
	// so half must be at least len(data) + len(summary) - limit.
	minHalf := len(data) + len(summary) - int(limit)
	if minHalf < 0 {
		minHalf = 0
	}

	// Truncate to the second half (or more if needed), preserving line boundaries.
	half := len(data) / 2
	if half < minHalf {
		half = minHalf
	}
	origHalf := half
	for half < len(data) && data[half] != '\n' {
		half++
	}
	if half >= len(data) {
		// No newline found in second half; keep from original midpoint.
		half = origHalf
	} else {
		half++ // skip the newline
	}

	if half > len(data) {
		half = len(data)
	}

	newData := make([]byte, 0, len(summary)+len(data)-half)
	newData = append(newData, summary...)
	newData = append(newData, data[half:]...)
	return os.WriteFile(m.path, newData, 0o600) //nolint:gosec // path is controlled by broker
}
