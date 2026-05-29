package cycle

import (
	"context"
	"errors"
	"testing"
)

// mockRunner is a test double for cmdRunner.
type mockRunner struct {
	responses map[string][]byte
	err       error
}

func (m mockRunner) Run(_ context.Context, name string, arg ...string) ([]byte, error) {
	key := name + " " + joinArgs(arg)
	if m.err != nil {
		return nil, m.err
	}
	if out, ok := m.responses[key]; ok {
		return out, nil
	}
	return []byte{}, nil
}

func joinArgs(arg []string) string {
	out := ""
	for i, a := range arg {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func TestDiscoverNextGoalWithRunner(t *testing.T) {
	ctx := context.Background()

	// go vet finds something.
	r := mockRunner{responses: map[string][]byte{
		"go vet ./...": []byte("pkg/foo.go:10:5: undefined: bar\n"),
	}}
	goal, err := discoverNextGoalWithRunner(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goal != "fix(vet): pkg/foo.go:10:5: undefined: bar" {
		t.Fatalf("unexpected goal: %q", goal)
	}

	// go vet clean, lint finds something.
	r = mockRunner{responses: map[string][]byte{
		"go vet ./...":                  []byte(""),
		"golangci-lint run --fast-only": []byte("pkg/bar.go:20:3: shadowed variable\n"),
	}}
	goal, err = discoverNextGoalWithRunner(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goal != "fix(lint): pkg/bar.go:20:3: shadowed variable" {
		t.Fatalf("unexpected goal: %q", goal)
	}

	// Both clean, no goal.
	r = mockRunner{responses: map[string][]byte{
		"go vet ./...":                  []byte(""),
		"golangci-lint run --fast-only": []byte(""),
		"git diff --name-only HEAD~1":   []byte(""),
	}}
	goal, err = discoverNextGoalWithRunner(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goal != "" {
		t.Fatalf("expected empty goal, got %q", goal)
	}
}

func TestParseGoVetOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"issue found", "pkg/foo.go:10:5: undefined: bar\n", "fix(vet): pkg/foo.go:10:5: undefined: bar"},
		{"clean", "", ""},
		{"only whitespace", "\n\n  \n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGoVetOutput([]byte(tt.in))
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLintOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"issue found", "pkg/bar.go:20:3: shadowed variable\n", "fix(lint): pkg/bar.go:20:3: shadowed variable"},
		{"clean", "", ""},
		{"level line skipped", "level=info msg=\" Running linters\"\npkg/b.go:1:1: err\n", "fix(lint): pkg/b.go:1:1: err"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLintOutput([]byte(tt.in))
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanTODOsGitError(t *testing.T) {
	ctx := context.Background()
	r := mockRunner{err: errors.New("git not found")}
	_, err := scanTODOs(ctx, r)
	if err == nil {
		t.Fatal("expected error when git fails")
	}
}

func TestScanFilesForTODOs(t *testing.T) {
	// This test needs real files on disk; skip in short mode.
	if testing.Short() {
		t.Skip("skipping file-based TODO scan in short mode")
	}
	got := scanFilesForTODOs([]byte("autogoal.go"))
	// autogoal.go itself contains "TODO" in the comment scan logic.
	if got == "" {
		t.Fatal("expected to find TODO in autogoal.go")
	}
	if !contains(got, "fix(todo):") {
		t.Fatalf("expected fix(todo:) prefix, got %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && contains(s[1:], substr)
}
