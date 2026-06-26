//go:build integration

package kimi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// TestIntegrationAdapterProbeAndRun exercises the real Kimi CLI subprocess path.
// It is gated by the build tag `integration` and the env var RALLISH_IT=1 so CI
// and ordinary test runs do not require an authenticated Kimi CLI.
func TestIntegrationAdapterProbeAndRun(t *testing.T) {
	if os.Getenv("RALLISH_IT") != "1" {
		t.Skip("set RALLISH_IT=1 to run real-adapter integration tests")
	}

	a, err := New()
	if err != nil {
		t.Skipf("kimi binary not on PATH: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Probe verifies auth/reachability cheaply.
	if err := a.Probe(ctx); err != nil {
		if isAuthLike(err) {
			t.Fatalf("kimi probe auth error: %v", err)
		}
		t.Fatalf("kimi probe failed: %v", err)
	}

	// Run a real turn. We only require that the subprocess path works and the
	// error, if any, is not an auth/rate-limit failure. Parsing the free-form
	// LLM output into TurnResponse is best-effort here.
	req := contract.TurnRequest{
		Turn: 1,
		Role: "executor",
		Task: contract.Task{
			Title: "integration probe",
			Body:  "Return a single fenced JSON object with keys summary (string) and done (boolean).",
		},
	}
	resp, err := a.Run(ctx, req)
	if err != nil {
		if isAuthLike(err) {
			t.Fatalf("kimi Run auth error: %v", err)
		}
		t.Logf("kimi Run returned non-auth error (parse/noise acceptable in integration): %v", err)
		return
	}
	if resp.Summary == "" {
		t.Fatalf("kimi Run returned empty summary")
	}
}

func isAuthLike(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "authenticated") ||
		strings.Contains(s, "not authenticated") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate-limit")
}
