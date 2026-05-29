package doctor

import (
	"context"
	"testing"
)

func TestInspect(t *testing.T) {
	rep, err := Inspect(context.Background(), "")
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if len(rep.Checks) == 0 {
		t.Fatal("expected at least one check")
	}
	for _, c := range rep.Checks {
		if c.Name == "" {
			t.Fatal("check name should not be empty")
		}
	}
}
