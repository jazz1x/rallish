package cycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// discoverNextGoal scans the codebase for actionable issues and returns a
// one-sentence goal for the next cycle. An empty string means no more work
// was found (clean codebase under the chosen heuristics).
func discoverNextGoal(ctx context.Context, _ State) (string, error) {
	// Try strategies in order of speed and reliability.
	if goal, err := runGoVet(ctx); err == nil && goal != "" {
		return goal, nil
	}
	if goal, err := runGolangciLintFast(ctx); err == nil && goal != "" {
		return goal, nil
	}
	if goal, err := scanTODOs(ctx); err == nil && goal != "" {
		return goal, nil
	}
	return "", nil
}

// runGoVet executes "go vet ./..." and returns the first issue as a goal.
func runGoVet(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	out, _ := cmd.CombinedOutput() // go vet returns exit 1 when issues exist.
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Typical line: pkg/foo/bar.go:42:12: shadowed variable
		return fmt.Sprintf("fix(vet): %s", line), nil
	}
	return "", nil
}

// runGolangciLintFast executes the fast linter set and returns the first issue.
func runGolangciLintFast(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// Prefer toolchain local binary, fallback to PATH.
	bin := ".toolchain/bin/golangci-lint"
	if _, err := os.Stat(bin); err != nil {
		bin = "golangci-lint"
	}

	cmd := exec.CommandContext(ctx, bin, "run", "--fast", "--out-format=line-number")
	out, _ := cmd.CombinedOutput() // exit 1 when issues exist.
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "level=") {
			continue
		}
		return fmt.Sprintf("fix(lint): %s", line), nil
	}
	return "", nil
}

// scanTODOs looks at files changed in the last commit for TODO/FIXME comments.
func scanTODOs(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "HEAD~1")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.Contains(line, "TODO") || strings.Contains(line, "FIXME") {
				return fmt.Sprintf("fix(todo): %s:%d", file, i+1), nil
			}
		}
	}
	return "", nil
}
