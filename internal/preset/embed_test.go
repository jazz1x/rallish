package preset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEmbedded(t *testing.T) {
	presets, err := LoadEmbedded()
	require.NoError(t, err)
	require.Len(t, presets, 2)

	require.Contains(t, presets, "pair-review")
	require.Contains(t, presets, "solo-ralph")

	pr := presets["pair-review"]
	require.Equal(t, "pair-review", pr.Name)
	require.Len(t, pr.Roles, 3)

	sr := presets["solo-ralph"]
	require.Equal(t, "solo-ralph", sr.Name)
	require.Len(t, sr.Roles, 1)
}
