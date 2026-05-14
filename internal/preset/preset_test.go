package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_ValidGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "valid.yaml"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	got, err := Load(f)
	require.NoError(t, err)

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	goldenPath := filepath.Join("testdata", "valid.golden.json")
	wantJSON, err := os.ReadFile(goldenPath) //nolint:gosec // testdata golden file
	require.NoError(t, err)

	require.JSONEq(t, string(wantJSON), string(gotJSON))
}

func TestLoad_UnknownField(t *testing.T) {
	yaml := `name: test
unknown_key: value
roles:
  - id: a
    runtime: claude
budget:
  max_turns: 1
  max_tokens: 1
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown field")
}

func TestLoad_MissingName(t *testing.T) {
	yaml := `roles:
  - id: a
    runtime: claude
budget:
  max_turns: 1
  max_tokens: 1
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
}

func TestLoad_NoRoles(t *testing.T) {
	yaml := `name: test
budget:
  max_turns: 1
  max_tokens: 1
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "role")
}

func TestLoad_BadRouting(t *testing.T) {
	yaml := `name: test
roles:
  - id: a
    runtime: claude
routing: bad_value
budget:
  max_turns: 1
  max_tokens: 1
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "routing")
}

func TestLoad_ZeroTurns(t *testing.T) {
	yaml := `name: test
roles:
  - id: a
    runtime: claude
budget:
  max_turns: 0
  max_tokens: 1
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_turns")
}

func TestLoad_ZeroTokens(t *testing.T) {
	yaml := `name: test
roles:
  - id: a
    runtime: claude
budget:
  max_turns: 1
  max_tokens: 0
  deadline_minutes: 1
`
	_, err := Load(strings.NewReader(yaml))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_tokens")
}
