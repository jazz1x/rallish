package claude

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdapterCheckMissingBinary(t *testing.T) {
	a := &Adapter{binary: "/nonexistent/claude/binary"}
	err := a.Check()
	require.Error(t, err)
}
