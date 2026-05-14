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
	if info.Size() <= m.maxKB*1024 {
		return nil
	}

	data, err := os.ReadFile(m.path) //nolint:gosec // path is controlled by broker
	if err != nil {
		return err
	}

	// Truncate to the second half, preserving line boundaries.
	half := len(data) / 2
	for half < len(data) && data[half] != '\n' {
		half++
	}
	if half < len(data) {
		half++ // skip the newline
	}

	summary := []byte("<!-- Scratchpad auto-compacted; older content truncated -->\n\n")
	newData := make([]byte, 0, len(summary)+len(data)-half)
	newData = append(newData, summary...)
	newData = append(newData, data[half:]...)
	return os.WriteFile(m.path, newData, 0o600) //nolint:gosec // path is controlled by broker
}
