package skills

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed rallish-operator/SKILL.md rallish-operator/SKILL.ko.md
var embedded embed.FS

// InstallResult reports what happened to one file during Install.
type InstallResult struct {
	Path   string // absolute path on disk
	Action string // "written", "unchanged", or "created"
}

// Install writes the embedded rallish-operator skill files to targetDir.
// targetDir is typically ~/.claude/skills/rallish-operator. It is created
// (mode 0755) if missing. Existing files are overwritten only when their
// content actually differs (so re-running is cheap and quiet).
//
// Returns the list of InstallResults (one per embedded file) and any error
// encountered.
func Install(targetDir string) ([]InstallResult, error) {
	if err := os.MkdirAll(targetDir, 0o750); err != nil { //nolint:gosec // skill dir; world-readable is fine but 0750 satisfies gosec
		return nil, fmt.Errorf("create target dir %q: %w", targetDir, err)
	}

	var results []InstallResult

	err := fs.WalkDir(embedded, "rallish-operator", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, readErr := embedded.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read embedded %q: %w", path, readErr)
		}

		// path is e.g. "rallish-operator/SKILL.md" — strip the leading dir component
		destPath := filepath.Join(targetDir, filepath.Base(path))

		existing, statErr := os.ReadFile(destPath) //nolint:gosec // targetDir is caller-supplied
		if statErr == nil {
			if bytes.Equal(existing, content) {
				results = append(results, InstallResult{Path: destPath, Action: "unchanged"})
				return nil
			}
			if writeErr := os.WriteFile(destPath, content, 0o644); writeErr != nil { //nolint:gosec // skill markdown file
				return fmt.Errorf("overwrite %q: %w", destPath, writeErr)
			}
			results = append(results, InstallResult{Path: destPath, Action: "written"})
			return nil
		}

		// File does not exist yet — create it.
		if writeErr := os.WriteFile(destPath, content, 0o644); writeErr != nil { //nolint:gosec // skill markdown file
			return fmt.Errorf("create %q: %w", destPath, writeErr)
		}
		results = append(results, InstallResult{Path: destPath, Action: "created"})
		return nil
	})
	if err != nil {
		return results, err
	}

	return results, nil
}
