package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

// stubBinary writes an executable shell script that prints the given stdout and
// exits 0, then returns its path. Lets us exercise Run without a real claude CLI.
func stubBinary(t *testing.T, stdout string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-stub.sh")
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	//nolint:gosec // G306 — a test stub binary must carry the executable bit to run
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	return path
}

// An exit-0 CLI that prints an auth notice instead of JSON must yield an
// actionable "not authenticated" error, not the cryptic parse failure (AC-F4.2).
func TestRunUnauthenticatedActionable(t *testing.T) {
	a := &Adapter{binary: stubBinary(t, "Error: Invalid API key provided")}
	_, err := a.Run(context.Background(), contract.TurnRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not authenticated")
	require.NotContains(t, err.Error(), "no JSON TurnResponse")
}

// A rate-limit notice classifies as a limit, not auth.
func TestRunRateLimitedActionable(t *testing.T) {
	a := &Adapter{binary: stubBinary(t, "Claude usage limit reached for this period")}
	_, err := a.Run(context.Background(), contract.TurnRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate or usage limit")
}

// Unrecognized non-JSON output keeps the diagnostic snippet path.
func TestRunUnknownKeepsSnippet(t *testing.T) {
	a := &Adapter{binary: stubBinary(t, "some unexpected banner text")}
	_, err := a.Run(context.Background(), contract.TurnRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parsing response")
}

func TestAdapterCheckMissingBinary(t *testing.T) {
	a := &Adapter{binary: "/nonexistent/claude/binary"}
	err := a.Check()
	require.Error(t, err)
}

func TestBuildCmdSetsDir(t *testing.T) {
	a := &Adapter{binary: "/bin/echo"}
	req := contract.TurnRequest{
		Task: contract.Task{RepoRoot: "/tmp/repo"},
	}
	cmd, err := a.buildCmd(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "/tmp/repo", cmd.Dir)
}

func TestBuildCmdEmptyDir(t *testing.T) {
	a := &Adapter{binary: "/bin/echo"}
	req := contract.TurnRequest{
		Task: contract.Task{RepoRoot: ""},
	}
	cmd, err := a.buildCmd(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, cmd.Dir)
}
