package claude

import (
	"context"
	"testing"

	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

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
