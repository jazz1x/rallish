package gates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// setupGitRepo creates a temporary git repository with an initial commit.
// The caller is responsible for checking out a feature branch if needed.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "init")
	// PreflightGate runs git against the process working directory, so move the
	// test into the temp repo for the duration of the test.
	t.Chdir(dir)
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test helper executing git in a temp repo
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output() // #nosec G204 -- test helper reading git branch in a temp repo
	if err != nil {
		t.Fatalf("git branch: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func makeState(id, goal string) cycle.State {
	return cycle.State{CycleState: contract.CycleState{ID: id, NextCycleGoal: goal}}
}

func makeStateWithBaseline(id, goal, baseline string) cycle.State {
	return cycle.State{CycleState: contract.CycleState{ID: id, NextCycleGoal: goal, BaselineSHA: baseline}}
}

func TestPreflightGateSuccess(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-test")

	state := makeState("cyc_preflight_ok", "feat: test")
	result, next := PreflightGate{}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %#v", result, result.Report())
	}
	if next.Branch != "feat-test" {
		t.Fatalf("branch = %q, want feat-test", next.Branch)
	}
	if next.BaselineSHA == "" {
		t.Fatalf("baseline SHA not set")
	}
}

func TestPreflightGateRejectsMain(t *testing.T) {
	dir := setupGitRepo(t)
	if got := currentBranch(t, dir); got != "main" {
		t.Fatalf("setup branch = %q, want main", got)
	}

	state := makeState("cyc_preflight_main", "feat: test")
	result, _ := PreflightGate{}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltPreflightFailed {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltPreflightFailed)
	}
	if !strings.Contains(result.Report().Stderr, "forbidden") {
		t.Fatalf("stderr = %q, want forbidden branch message", result.Report().Stderr)
	}
}

func TestPreflightGateRejectsMaster(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "master")

	state := makeState("cyc_preflight_master", "feat: test")
	result, _ := PreflightGate{}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltPreflightFailed {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltPreflightFailed)
	}
}

func TestPreflightGateRejectsDirtyTree(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-test")
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	state := makeState("cyc_preflight_dirty", "feat: test")
	result, _ := PreflightGate{}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltPreflightFailed {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltPreflightFailed)
	}
}

func TestPreflightGateRejectsEmptyGoal(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-test")

	state := makeState("cyc_preflight_empty", "   ")
	result, _ := PreflightGate{}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltPreflightFailed {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltPreflightFailed)
	}
}

func TestPreflightGateSetsBaselineSHAOnlyOnce(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-test")

	state := makeStateWithBaseline("cyc_preflight_baseline", "feat: test", "sha1")
	result, next := PreflightGate{}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %#v", result, result.Report())
	}
	if next.BaselineSHA != "sha1" {
		t.Fatalf("baseline SHA = %q, want sha1 (must not overwrite)", next.BaselineSHA)
	}
}
