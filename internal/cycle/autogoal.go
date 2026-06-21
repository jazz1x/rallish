package cycle

import (
	"context"
	"errors"
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
	// Strategies in order of speed and reliability. A strategy returns a non-nil
	// error only when it could not run to completion (missing toolchain, timeout,
	// git failure); a tool that runs and reports findings is success with a
	// non-empty goal.
	strategies := []func(context.Context, cmdRunner) (string, error){
		runGoVet, runGolangciLintFast, scanTODOs,
	}

	var ranAny bool
	var errs []error
	for _, run := range strategies {
		goal, err := run(ctx, r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ranAny = true
		if goal != "" {
			return goal, nil
		}
	}

	if !ranAny {
		// Every strategy failed to run. Returning "" here would be read by the
		// driver as HaltSuccess ("no work left") — a broken environment must not
		// masquerade as a clean, completed codebase. Surface the failure instead.
		return "", fmt.Errorf("goal discovery: all strategies failed to run: %w", errors.Join(errs...))
	}
	return "", nil
}

// isExitError reports whether err is (or wraps) an *exec.ExitError — i.e. the
// command ran to completion and exited non-zero. For go vet and golangci-lint a
// non-zero exit is the normal "issues found" signal, not a failure to run.
func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// classifyRunErr decides how to treat the error from a discovery command whose
// non-zero exit is the expected "issues found" signal. It returns nil when the
// output should be parsed (clean exit, or an *exec.ExitError reporting findings)
// and a labelled error when the command could not run to completion (context
// cancelled/expired, missing binary, or any other failure) so discovery can
// tell a broken environment from a clean tree.
func classifyRunErr(ctx context.Context, label string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", label, ctxErr)
	}
	if err != nil && !isExitError(err) {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// runGoVet executes "go vet ./..." and returns the first issue as a goal.
func runGoVet(ctx context.Context, r cmdRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	out, err := r.Run(ctx, "go", "vet", "./...") // go vet exits non-zero when issues exist.
	if cerr := classifyRunErr(ctx, "go vet", err); cerr != nil {
		return "", cerr
	}
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

	out, err := r.Run(ctx, bin, "run", "--fast-only") // exits non-zero when issues exist.
	if cerr := classifyRunErr(ctx, "golangci-lint", err); cerr != nil {
		return "", cerr
	}
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
