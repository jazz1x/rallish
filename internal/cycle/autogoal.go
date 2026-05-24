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

// cmdRunner abstracts exec.CommandContext for testability.
type cmdRunner interface {
	Run(ctx context.Context, name string, arg ...string) ([]byte, error)
}

// defaultRunner is the production implementation of cmdRunner.
type defaultRunner struct{}

func (defaultRunner) Run(ctx context.Context, name string, arg ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arg...).CombinedOutput() //nolint:gosec // hardcoded commands in discoverNextGoal
}

// discoverNextGoal scans the codebase for actionable issues and returns a
// one-sentence goal for the next cycle. An empty string means no more work
// was found (clean codebase under the chosen heuristics).
func discoverNextGoal(ctx context.Context, _ State) (string, error) {
	return discoverNextGoalWithRunner(ctx, defaultRunner{})
}

func discoverNextGoalWithRunner(ctx context.Context, r cmdRunner) (string, error) {
	// Try strategies in order of speed and reliability.
	if goal, err := runGoVet(ctx, r); err == nil && goal != "" {
		return goal, nil
	}
	if goal, err := runGolangciLintFast(ctx, r); err == nil && goal != "" {
		return goal, nil
	}
	if goal, err := scanTODOs(ctx, r); err == nil && goal != "" {
		return goal, nil
	}
	return "", nil
}

// runGoVet executes "go vet ./..." and returns the first issue as a goal.
func runGoVet(ctx context.Context, r cmdRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	out, _ := r.Run(ctx, "go", "vet", "./...") // go vet returns exit 1 when issues exist.
	return parseGoVetOutput(out), nil
}

func parseGoVetOutput(out []byte) string {
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Typical line: pkg/foo/bar.go:42:12: shadowed variable
		return fmt.Sprintf("fix(vet): %s", line)
	}
	return ""
}

// runGolangciLintFast executes the fast linter set and returns the first issue.
func runGolangciLintFast(ctx context.Context, r cmdRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// Prefer toolchain local binary, fallback to PATH.
	bin := ".toolchain/bin/golangci-lint"
	if _, err := os.Stat(bin); err != nil {
		bin = "golangci-lint"
	}

	out, _ := r.Run(ctx, bin, "run", "--fast-only") // exit 1 when issues exist.
	return parseLintOutput(out), nil
}

func parseLintOutput(out []byte) string {
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "level=") {
			continue
		}
		return fmt.Sprintf("fix(lint): %s", line)
	}
	return ""
}

// scanTODOs looks at files changed in the last commit for TODO/FIXME comments.
func scanTODOs(ctx context.Context, r cmdRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := r.Run(ctx, "git", "diff", "--name-only", "HEAD~1")
	if err != nil {
		return "", err
	}
	return scanFilesForTODOs(out), nil
}

func scanFilesForTODOs(out []byte) string {
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
				return fmt.Sprintf("fix(todo): %s:%d", file, i+1)
			}
		}
	}
	return ""
}
