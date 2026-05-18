package skills

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:rallish
var embedded embed.FS

// InstallResult reports what happened to one file during Install.
type InstallResult struct {
	Path   string // absolute path on disk
	Action string // "written", "unchanged", or "created"
}

// Install writes the embedded rallish skill files to targetDir.
// targetDir is typically ~/.claude/skills/rallish. It is created
// (mode 0755) if missing. Existing files are overwritten only when their
// content actually differs (so re-running is cheap and quiet).
//
// Files under scripts/ are written with mode 0755 (executable); all other
// files use mode 0644.
//
// Returns the list of InstallResults (one per embedded file) and any error
// encountered.
func Install(targetDir string) ([]InstallResult, error) {
	if err := os.MkdirAll(targetDir, 0o750); err != nil { //nolint:gosec // skill dir; world-readable is fine but 0750 satisfies gosec
		return nil, fmt.Errorf("create target dir %q: %w", targetDir, err)
	}

	var results []InstallResult

	err := fs.WalkDir(embedded, "rallish", func(path string, d fs.DirEntry, err error) error {
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

		// Strip leading "rallish/" prefix to get relative dest path.
		rel := strings.TrimPrefix(path, "rallish/")

		destPath := filepath.Join(targetDir, rel)

		// Ensure parent directory exists.
		if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o750); mkErr != nil { //nolint:gosec // skill subdir
			return fmt.Errorf("create parent dir for %q: %w", destPath, mkErr)
		}

		// Determine file mode: executable for files under scripts/.
		mode := fs.FileMode(0o644)
		if strings.HasPrefix(rel, "scripts/") {
			mode = 0o755
		}

		existing, statErr := os.ReadFile(destPath) //nolint:gosec // targetDir is caller-supplied
		if statErr == nil {
			if bytes.Equal(existing, content) {
				results = append(results, InstallResult{Path: destPath, Action: "unchanged"})
				return nil
			}
			if writeErr := os.WriteFile(destPath, content, mode); writeErr != nil { //nolint:gosec // skill file
				return fmt.Errorf("overwrite %q: %w", destPath, writeErr)
			}
			results = append(results, InstallResult{Path: destPath, Action: "written"})
			return nil
		}

		// File does not exist yet — create it.
		if writeErr := os.WriteFile(destPath, content, mode); writeErr != nil { //nolint:gosec // skill file
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
