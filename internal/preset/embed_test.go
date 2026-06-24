package preset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEmbedded(t *testing.T) {
	presets, err := LoadEmbedded()
	require.NoError(t, err)
	require.Len(t, presets, 3)

	require.Contains(t, presets, "pair-review")
	require.Contains(t, presets, "solo-ralph")
	require.Contains(t, presets, "fake-demo")

	pr := presets["pair-review"]
	require.Equal(t, "pair-review", pr.Name)
	require.Len(t, pr.Roles, 3)

	sr := presets["solo-ralph"]
	require.Equal(t, "solo-ralph", sr.Name)
	require.Len(t, sr.Roles, 1)

	// fake-demo is the credential-free smoke test: a single role on the in-process
	// `fake` runtime, so a stranger can verify an install without any agent CLI.
	fd := presets["fake-demo"]
	require.Equal(t, "fake-demo", fd.Name)
	require.Len(t, fd.Roles, 1)
	require.Equal(t, "fake", fd.Roles[0].Runtime)
}
