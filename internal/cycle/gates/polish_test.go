package gates

import "testing"

func TestHasConventionalPrefix(t *testing.T) {
	prefixes := []string{
		"feat:", "fix:", "refactor:", "docs:", "test:",
		"chore:", "sec:", "ci:", "build:", "perf:", "style:",
	}
	for _, p := range prefixes {
		if !hasConventionalPrefix(p) {
			t.Errorf("hasConventionalPrefix(%q) = false, want true", p)
		}
	}

	if hasConventionalPrefix("") {
		t.Error("empty string should not have conventional prefix")
	}
	if hasConventionalPrefix("no prefix") {
		t.Error("'no prefix' should not have conventional prefix")
	}
	if hasConventionalPrefix("unknown: blah") {
		t.Error("'unknown:' should not have conventional prefix")
	}
}
