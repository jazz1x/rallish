package adapter

import (
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestParseLastJSONBlock_Fenced(t *testing.T) {
	out := []byte("blah blah\n```json\n{\"done\":true,\"summary\":\"ok\",\"self_eval\":\"confident\"}\n```\n")
	var resp contract.TurnResponse
	require.NoError(t, ParseLastJSONBlock(out, &resp))
	require.True(t, resp.Done)
	require.Equal(t, "ok", resp.Summary)
}

func TestParseLastJSONBlock_BareJSONFallback(t *testing.T) {
	out := []byte("Some preamble.\n{\"done\":false,\"handoff_to\":\"beta\",\"summary\":\"pass\",\"self_eval\":\"confident\"}\nTrailer.\n")
	var resp contract.TurnResponse
	require.NoError(t, ParseLastJSONBlock(out, &resp))
	require.False(t, resp.Done)
	require.Equal(t, "beta", resp.HandoffTo)
}

func TestParseLastJSONBlock_PrefersLast(t *testing.T) {
	out := []byte("{\"done\":false,\"summary\":\"first\",\"self_eval\":\"confident\"}\n{\"done\":true,\"summary\":\"last\",\"self_eval\":\"confident\"}\n")
	var resp contract.TurnResponse
	require.NoError(t, ParseLastJSONBlock(out, &resp))
	require.True(t, resp.Done)
	require.Equal(t, "last", resp.Summary)
}

func TestParseLastJSONBlock_IgnoresBracesInStrings(t *testing.T) {
	out := []byte("noise { not json\n{\"done\":true,\"summary\":\"has } and { in it\",\"self_eval\":\"confident\"}\n")
	var resp contract.TurnResponse
	require.NoError(t, ParseLastJSONBlock(out, &resp))
	require.True(t, resp.Done)
	require.Equal(t, "has } and { in it", resp.Summary)
}

func TestParseLastJSONBlock_NoJSON(t *testing.T) {
	out := []byte("Sorry, I cannot comply.")
	var resp contract.TurnResponse
	require.Error(t, ParseLastJSONBlock(out, &resp))
}
