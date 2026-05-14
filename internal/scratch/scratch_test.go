package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "scratch.md"), 64)

	if err := m.Append("line one\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := m.Append("line two\n"); err != nil {
		t.Fatalf("append: %v", err)
	}

	content, err := m.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(content, "line one") {
		t.Fatalf("expected line one in scratch")
	}
	if !strings.Contains(content, "line two") {
		t.Fatalf("expected line two in scratch")
	}
}

func TestManagerCompaction(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "scratch.md"), 1) // 1 KB limit

	// Write enough data to exceed 1 KB.
	big := strings.Repeat("a", 1024)
	if err := m.Append(big); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := m.Append(big); err != nil {
		t.Fatalf("append: %v", err)
	}

	info, err := os.Stat(m.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 1024 {
		t.Fatalf("scratchpad size %d exceeds 1 KB limit after compaction", info.Size())
	}

	content, err := m.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(content, "compacted") {
		t.Fatalf("expected compaction marker in scratch")
	}
}
