package contract

import (
	"strings"
	"testing"
)

func TestTurnResponse_Compact(t *testing.T) {
	resp := TurnResponse{
		Done:      true,
		HandoffTo: "reviewer",
		Summary:   "fixed bug",
		Artifacts: []string{"fix.go"},
		SelfEval:  SelfEvalConfident,
		Usage:     &Usage{TokensIn: 100, TokensOut: 50},
	}
	got := resp.Compact()
	wantParts := []string{
		"[done]",
		"→ reviewer",
		"eval=confident",
		`summary="fixed bug"`,
		"artifacts=[fix.go]",
		"usage=100/50",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Errorf("Compact() = %q, missing %q", got, part)
		}
	}
}

func TestTurnResponse_Compact_Empty(t *testing.T) {
	resp := TurnResponse{}
	got := resp.Compact()
	if got != "eval=" {
		t.Errorf("Compact() = %q, want %q", got, "eval=")
	}
}

func BenchmarkTurnResponse_Compact(b *testing.B) {
	resp := TurnResponse{
		Done:      true,
		HandoffTo: "reviewer",
		Summary:   "fixed bug",
		Artifacts: []string{"fix.go", "test.go"},
		SelfEval:  SelfEvalConfident,
		Usage:     &Usage{TokensIn: 100, TokensOut: 50},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = resp.Compact()
	}
}
