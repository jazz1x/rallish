package gates

import (
	"testing"
)

func TestStandardPipelineOrder(t *testing.T) {
	p := StandardPipeline("", "", nil)
	want := []string{"preflight", "audit", "philosophy", "polish", "commit"}
	if len(p) != len(want) {
		t.Fatalf("pipeline length = %d, want %d", len(p), len(want))
	}
	for i, gate := range p {
		if gate.Name() != want[i] {
			t.Fatalf("gate %d = %q, want %q", i, gate.Name(), want[i])
		}
	}
}

func TestStandardPipelineInsertsLocalGatesAfterAudit(t *testing.T) {
	p := StandardPipeline("", "", []string{"go env GOMOD", "go version"})
	names := make([]string, len(p))
	for i, g := range p {
		names[i] = g.Name()
	}
	want := []string{"preflight", "audit", "cmd:go env GOMOD", "cmd:go version", "philosophy", "polish", "commit"}
	if len(names) != len(want) {
		t.Fatalf("pipeline length = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Fatalf("gate %d = %q, want %q", i, n, want[i])
		}
	}
}

func TestStandardPipelineAuditOverrideDoesNotAffectOrder(t *testing.T) {
	p := StandardPipeline("go env GOMOD", "go test ./...", []string{"go version"})
	names := make([]string, len(p))
	for i, g := range p {
		names[i] = g.Name()
	}
	want := []string{"preflight", "audit", "cmd:go version", "philosophy", "polish", "commit"}
	if len(names) != len(want) {
		t.Fatalf("pipeline length = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Fatalf("gate %d = %q, want %q", i, n, want[i])
		}
	}
}
