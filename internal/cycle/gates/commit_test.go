package gates

import (
	"testing"
)

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
