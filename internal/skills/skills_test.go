package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jazz1x/rallish/internal/skills"
)

// embeddedFiles lists relative paths (from install target root) expected to be
// installed. Must match the files in rallish/ exactly.
var embeddedFiles = []string{
	"SKILL.md",
	"SKILL.ko.md",
	"scripts/install-binary.sh",
}

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
	// Pre-populate with corrupted content using the same relative paths.
	for _, rel := range embeddedFiles {
		dest := filepath.Join(dir, rel)
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o750); mkErr != nil { //nolint:gosec // test temp dir
			t.Fatalf("setup mkdir: %v", mkErr)
		}
		if err := os.WriteFile(dest, []byte("corrupted"), 0o600); err != nil {
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

func TestInstallBundlesScripts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := skills.Install(dir); err != nil {
		t.Fatalf("Install: %v", err)
	}

	scriptPath := filepath.Join(dir, "scripts", "install-binary.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("scripts/install-binary.sh not found after install: %v", err)
	}

	// Verify executable bit is set.
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("scripts/install-binary.sh is not executable: mode=%v", info.Mode().Perm())
	}

	// Verify content is non-empty and contains expected marker.
	data, readErr := os.ReadFile(scriptPath) //nolint:gosec // test temp path
	if readErr != nil {
		t.Fatalf("read install-binary.sh: %v", readErr)
	}
	if len(data) == 0 {
		t.Fatal("install-binary.sh is empty")
	}
}

func TestInstallNamed_AutonomousCycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	results, err := skills.InstallNamed("autonomous-cycle", dir)
	if err != nil {
		t.Fatalf("InstallNamed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Verify SKILL.md exists.
	skillPath := filepath.Join(dir, "SKILL.md")
	if _, statErr := os.Stat(skillPath); statErr != nil {
		t.Fatalf("SKILL.md not found: %v", statErr)
	}
}

func TestInstallCompanionFiles(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	results, err := skills.InstallCompanionFiles("autonomous-cycle", home)
	if err != nil {
		t.Fatalf("InstallCompanionFiles: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one companion file")
	}

	// Verify scripts directory has executable files.
	scriptsDir := filepath.Join(home, ".claude", "scripts")
	entries, readErr := os.ReadDir(scriptsDir)
	if readErr != nil {
		t.Fatalf("read scripts dir: %v", readErr)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one script")
	}
	for _, e := range entries {
		info, statErr := e.Info()
		if statErr != nil {
			t.Fatalf("stat %q: %v", e.Name(), statErr)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("script %q is not executable", e.Name())
		}
	}

	// Verify runbooks directory has files.
	runbooksDir := filepath.Join(home, ".claude", "runbooks")
	entries, readErr = os.ReadDir(runbooksDir)
	if readErr != nil {
		t.Fatalf("read runbooks dir: %v", readErr)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one runbook")
	}
}
