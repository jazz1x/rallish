package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jazz1x/rallish/internal/skills"
)

// embeddedFiles lists file basenames expected to be installed.
var embeddedFiles = []string{"SKILL.md", "SKILL.ko.md"}

func TestInstall_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	results, err := skills.Install(dir)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if len(results) != len(embeddedFiles) {
		t.Fatalf("expected %d results, got %d", len(embeddedFiles), len(results))
	}
	for _, r := range results {
		if r.Action != "created" {
			t.Errorf("expected action=created for %q, got %q", r.Path, r.Action)
		}
		data, readErr := os.ReadFile(r.Path)
		if readErr != nil {
			t.Errorf("file not readable after install: %v", readErr)
		}
		if len(data) == 0 {
			t.Errorf("file %q is empty after install", r.Path)
		}
	}
}

func TestInstall_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// First run.
	if _, err := skills.Install(dir); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	// Second run — all files should be unchanged.
	results, err := skills.Install(dir)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	for _, r := range results {
		if r.Action != "unchanged" {
			t.Errorf("expected action=unchanged on second run for %q, got %q", r.Path, r.Action)
		}
	}
}

func TestInstall_OverwritesCorrupted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pre-populate with corrupted content.
	for _, name := range embeddedFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("corrupted"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	results, err := skills.Install(dir)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, r := range results {
		if r.Action != "written" {
			t.Errorf("expected action=written for corrupted file %q, got %q", r.Path, r.Action)
		}
		data, readErr := os.ReadFile(r.Path)
		if readErr != nil {
			t.Fatalf("read after overwrite: %v", readErr)
		}
		if string(data) == "corrupted" {
			t.Errorf("file %q still contains corrupted content after install", r.Path)
		}
	}
}

func TestInstall_CreatesTargetDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "skill-dir")
	results, err := skills.Install(dir)
	if err != nil {
		t.Fatalf("Install with non-existent dir: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	for _, r := range results {
		if r.Action != "created" {
			t.Errorf("expected created, got %q for %s", r.Action, r.Path)
		}
	}
}
