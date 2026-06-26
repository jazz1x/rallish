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

func TestCommitGateCreatesCommit(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-commit")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	state := cycle.State{CycleState: contract.CycleState{ID: "cyc_commit", NextCycleGoal: "feat: add new file"}}
	result, _ := CommitGate{}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %#v", result, result.Report())
	}

	out, err := exec.Command("git", "-C", dir, "log", "--oneline", "-1").Output() // #nosec G204 -- test helper reading commit in a temp repo
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "feat: add new file") {
		t.Fatalf("commit message missing goal: %s", out)
	}
}

func TestCommitGateNothingToCommitIsSuccess(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-clean")

	state := cycle.State{CycleState: contract.CycleState{ID: "cyc_commit_clean", NextCycleGoal: "feat: no changes"}}
	result, _ := CommitGate{}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %#v", result, result.Report())
	}
}

func TestDeriveCommitMessage(t *testing.T) {
	tests := []struct {
		goal     string
		cycleNum int
		want     string
	}{
		{"", 1, "refactor: autonomous cycle step [cycle-1]"},
		{"feat: add helper", 2, "feat: add helper [cycle-2]"},
		{"fix bug", 3, "refactor: fix bug [cycle-3]"},
		{"  fix: trim spaces  ", 4, "fix: trim spaces [cycle-4]"},
		{"chore: update deps", 5, "chore: update deps [cycle-5]"},
		{"docs: readme", 6, "docs: readme [cycle-6]"},
		{"perf: optimise loop", 7, "perf: optimise loop [cycle-7]"},
		{"test: add coverage", 8, "test: add coverage [cycle-8]"},
		{"refactor: extract fn", 9, "refactor: extract fn [cycle-9]"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := deriveCommitMessage(tt.goal, tt.cycleNum)
			if got != tt.want {
				t.Fatalf("deriveCommitMessage(%q, %d) = %q, want %q", tt.goal, tt.cycleNum, got, tt.want)
			}
		})
	}
}
