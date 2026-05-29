package skills

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:rallish all:autonomous-cycle
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
	return InstallNamed("rallish", targetDir)
}

// InstallNamed writes the embedded skill named skillName to targetDir.
// Supported names: "rallish", "autonomous-cycle".
func InstallNamed(skillName, targetDir string) ([]InstallResult, error) {
	if err := os.MkdirAll(targetDir, 0o750); err != nil { //nolint:gosec // skill dir; world-readable is fine but 0750 satisfies gosec
		return nil, fmt.Errorf("create target dir %q: %w", targetDir, err)
	}

	var results []InstallResult

	err := fs.WalkDir(embedded, skillName, func(path string, d fs.DirEntry, err error) error {
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

		// Strip leading "<skillName>/" prefix to get relative dest path.
		rel := strings.TrimPrefix(path, skillName+"/")

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

// ListEmbeddedSkills returns the names of all embedded skill bundles.
func ListEmbeddedSkills() ([]string, error) {
	entries, err := embedded.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded root: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ReadEmbeddedSkill returns the raw bytes of an embedded skill file by path.
// The path is relative to the skill root, e.g. "autonomous-cycle/SKILL.md".
func ReadEmbeddedSkill(path string) ([]byte, error) {
	return embedded.ReadFile(path)
}

// MatchSkillByTrigger finds an embedded skill whose SKILL.md contains the given
// phrase in its triggers section or body.
func MatchSkillByTrigger(phrase string) (string, error) {
	names, err := ListEmbeddedSkills()
	if err != nil {
		return "", err
	}

	phraseLower := strings.ToLower(phrase)

	for _, name := range names {
		data, readErr := ReadEmbeddedSkill(name + "/SKILL.md")
		if readErr != nil {
			continue
		}
		content := string(data)

		// Extract YAML frontmatter.
		if !strings.HasPrefix(content, "---") {
			continue
		}
		endIdx := strings.Index(content[3:], "---")
		if endIdx == -1 {
			continue
		}
		frontmatter := content[3 : 3+endIdx]

		if containsTrigger(frontmatter, phraseLower) {
			return name, nil
		}

		// Fallback: check full body.
		if strings.Contains(strings.ToLower(content), phraseLower) {
			return name, nil
		}
	}

	return "", fmt.Errorf("no skill matched trigger phrase: %q", phrase)
}

func containsTrigger(frontmatter, phrase string) bool {
	lines := strings.Split(frontmatter, "\n")
	inTriggers := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Triggers:") {
			inTriggers = true
			rest := strings.ToLower(strings.TrimPrefix(line, "Triggers:"))
			if strings.Contains(rest, phrase) {
				return true
			}
			continue
		}
		if inTriggers {
			if strings.HasPrefix(line, "-") {
				triggerText := strings.ToLower(strings.TrimPrefix(line, "-"))
				triggerText = strings.TrimSpace(triggerText)
				triggerText = strings.Trim(triggerText, `"`)
				if strings.Contains(triggerText, phrase) || strings.Contains(phrase, triggerText) {
					return true
				}
			} else if line != "" && !strings.HasPrefix(line, "#") {
				inTriggers = false
			}
		}
	}
	return false
}

// InstallCompanionFiles installs scripts and runbooks from an embedded skill
// into the user's ~/.claude/scripts and ~/.claude/runbooks directories.
func InstallCompanionFiles(skillName, homeDir string) ([]InstallResult, error) {
	var results []InstallResult

	err := fs.WalkDir(embedded, skillName, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel := strings.TrimPrefix(path, skillName+"/")
		var destDir string
		mode := fs.FileMode(0o644)

		switch {
		case strings.HasPrefix(rel, "scripts/"):
			destDir = filepath.Join(homeDir, ".claude", "scripts")
			mode = 0o755
		case strings.HasPrefix(rel, "runbooks/"):
			destDir = filepath.Join(homeDir, ".claude", "runbooks")
		default:
			return nil // skip non-companion files
		}

		content, readErr := embedded.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read embedded %q: %w", path, readErr)
		}

		destPath := filepath.Join(destDir, filepath.Base(rel))
		if mkErr := os.MkdirAll(destDir, 0o750); mkErr != nil { //nolint:gosec // well-known dir
			return fmt.Errorf("create dir %q: %w", destDir, mkErr)
		}

		existing, statErr := os.ReadFile(destPath) //nolint:gosec // well-known path
		if statErr == nil {
			if bytes.Equal(existing, content) {
				results = append(results, InstallResult{Path: destPath, Action: "unchanged"})
				return nil
			}
			if writeErr := os.WriteFile(destPath, content, mode); writeErr != nil { //nolint:gosec // companion file
				return fmt.Errorf("overwrite %q: %w", destPath, writeErr)
			}
			results = append(results, InstallResult{Path: destPath, Action: "written"})
			return nil
		}

		if writeErr := os.WriteFile(destPath, content, mode); writeErr != nil { //nolint:gosec // companion file
			return fmt.Errorf("create %q: %w", destPath, writeErr)
		}
		results = append(results, InstallResult{Path: destPath, Action: "created"})
		return nil
	})

	return results, err
}

// SkillFileExists reports whether the given skill path exists in the embedded FS.
func SkillFileExists(path string) bool {
	_, err := embedded.ReadFile(path)
	return err == nil
}
