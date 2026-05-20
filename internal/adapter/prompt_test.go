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

func BenchmarkBuildPrompt(b *testing.B) {
	req := contract.TurnRequest{
		Session:     "sess-123",
		Turn:        5,
		Role:        "planner",
		RuntimeHint: "claude",
		ModelHint:   "claude-3-5-sonnet",
		Budget:      contract.Budget{TokensLeft: 8000, TurnsLeft: 10},
		LastTurn: &contract.LastTurn{
			From:      "executor",
			Summary:   "implemented feature X",
			Artifacts: []string{"feat.go"},
			SelfEval:  contract.SelfEvalConfident,
		},
		Task: contract.Task{
			Title:    "Add auth",
			Body:     "Implement OAuth2 flow",
			RepoRoot: "/tmp/repo",
		},
		ExitWhen: []contract.ExitCondition{contract.ExitTestsPass},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := BuildPrompt(req)
		require.NoError(b, err)
	}
}
