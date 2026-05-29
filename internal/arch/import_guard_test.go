// Package arch contains build-time architectural invariant tests.
//
// These tests enforce the non-goals stated in docs/north-star.md §Non-Goals:
//
//   - "❌ Not a loop / runtime / scheduler / cron"
//   - "❌ No graph-DB / subgraph-isomorphism / GNN dependency"
//   - "○ To be enforced: a CI import-guard test asserting the core imports no
//     loop/scheduler OR graph-DB package (via negativa as an invariant)."
//
// Scope rationale: pkg/contract is the unambiguous neutral SSOT — it must
// never grow a loop-engine, scheduler, or graph-DB dependency. internal/cycle
// is deliberately excluded because it legitimately imports internal/adapter
// (the reference driver coupling); that coupling is documented as a reference
// driver concern, not a core contract concern.
//
// How to verify the guard actually catches violations (manual procedure):
//
//  1. Temporarily add a blank import in pkg/contract/doc.go:
//     _ "github.com/robfig/cron/v3"
//  2. Run: go test ./internal/arch/ -run TestCoreImportGuard
//  3. The test must FAIL with the forbidden package name in the error.
//  4. Revert the change.
//
// The guard cannot silently become a no-op: TestForbiddenMatcher verifies the
// deny-list predicate itself catches every known category with a synthetic path.
package arch

import (
	"os/exec"
	"strings"
	"testing"
)

// forbiddenPrefixes is the SSOT deny-list for the core import guard.
// Each entry is tied to a north-star Non-Goal (docs/north-star.md §Non-Goals).
//
// Entries use go-module path prefixes (matched with strings.HasPrefix against
// each dep returned by `go list -deps`).
var forbiddenPrefixes = []struct {
	prefix string
	reason string // north-star Non-Goal citation
}{
	// Scheduler / cron / loop-engine libs — Non-Goal: "not a scheduler / cron"
	{"github.com/robfig/cron", "scheduler: robfig/cron — north-star: 'not a scheduler / cron'"},
	{"github.com/go-co-op/gocron", "scheduler: go-co-op/gocron — north-star: 'not a scheduler / cron'"},
	{"github.com/jasonlvhit/gocron", "scheduler: jasonlvhit/gocron — north-star: 'not a scheduler / cron'"},

	// Graph-database / graph-engine / subgraph-isomorphism libs —
	// Non-Goal: "No graph-DB / subgraph-isomorphism / GNN dependency"
	{"github.com/neo4j/neo4j-go-driver", "graph-DB: neo4j — north-star: 'No graph-DB/subgraph-iso/GNN dependency'"},
	{"github.com/cayleygraph/cayley", "graph-engine: cayley — north-star: 'No graph-DB/subgraph-iso/GNN dependency'"},
	{"github.com/dgraph-io/dgraph", "graph-DB: dgraph — north-star: 'No graph-DB/subgraph-iso/GNN dependency'"},
	{"gonum.org/v1/gonum/graph", "graph-engine: gonum/graph — north-star: 'No graph-DB/subgraph-iso/GNN dependency'"},

	// GNN / ML graph libs — Non-Goal: "No graph-DB / subgraph-isomorphism / GNN dependency"
	{"gorgonia.org/gorgonia", "GNN/ML: gorgonia — north-star: 'No graph-DB/subgraph-iso/GNN dependency'"},
}

// corePackages is the set of packages whose transitive closure must not contain
// any forbiddenPrefix. Scoped to pkg/contract (the neutral SSOT); internal/cycle
// is excluded — see package doc comment for rationale.
var corePackages = []string{
	"github.com/jazz1x/rallish/pkg/contract",
}

// TestCoreImportGuard asserts that the transitive import closure of the core
// packages contains none of the forbidden prefixes.
//
// It passes today (no violation) and must FAIL if a forbidden dep is added.
// The test uses `go list -deps` over the pinned toolchain — no new dependency
// on golang.org/x/tools is required.
func TestCoreImportGuard(t *testing.T) {
	t.Helper()
	for _, pkg := range corePackages {
		pkg := pkg
		t.Run(pkg, func(t *testing.T) {
			deps := listDeps(t, pkg)
			for _, dep := range deps {
				for _, fp := range forbiddenPrefixes {
					if strings.HasPrefix(dep, fp.prefix) {
						t.Errorf(
							"VIOLATION: core package %q transitively imports forbidden dep %q\n  rule: %s",
							pkg, dep, fp.reason,
						)
					}
				}
			}
		})
	}
}

// TestForbiddenMatcher is a self-test of the deny-list predicate.
// It guarantees the matcher is NOT a no-op: every forbidden category must be
// caught by at least one entry when presented with a synthetic import path.
func TestForbiddenMatcher(t *testing.T) {
	cases := []struct {
		syntheticDep string
		wantCaught   bool
	}{
		// Must catch:
		{"github.com/robfig/cron/v3", true},
		{"github.com/robfig/cron", true},
		{"github.com/go-co-op/gocron", true},
		{"github.com/neo4j/neo4j-go-driver/v5", true},
		{"github.com/cayleygraph/cayley/graph", true},
		{"github.com/dgraph-io/dgraph/client", true},
		{"gonum.org/v1/gonum/graph/simple", true},
		{"gorgonia.org/gorgonia", true},
		// Must NOT catch (allow-list examples — stdlib + our own imports):
		{"fmt", false},
		{"encoding/json", false},
		{"github.com/jazz1x/rallish/pkg/contract", false},
		{"github.com/jazz1x/rallish/internal/cycle", false},
		{"golang.org/x/sync", false},
		{"github.com/goccy/go-yaml", false},
		// Near-miss prefixes that must not false-positive:
		{"github.com/robfig/robfig-other", false},
		{"gonum.org/v1/gonum/stat", false}, // gonum/stat is fine; only gonum/graph is forbidden
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.syntheticDep, func(t *testing.T) {
			caught := isForbidden(tc.syntheticDep)
			if caught != tc.wantCaught {
				if tc.wantCaught {
					t.Errorf("deny-list missed forbidden dep %q — matcher is broken or entry is missing", tc.syntheticDep)
				} else {
					t.Errorf("deny-list false-positive on allowed dep %q", tc.syntheticDep)
				}
			}
		})
	}
}

// isForbidden reports whether dep matches any entry in forbiddenPrefixes.
func isForbidden(dep string) bool {
	for _, fp := range forbiddenPrefixes {
		if strings.HasPrefix(dep, fp.prefix) {
			return true
		}
	}
	return false
}

// listDeps returns the transitive import list for pkg via `go list -deps`.
// pkg is always a compile-time constant from corePackages (not user input).
func listDeps(t *testing.T, pkg string) []string {
	t.Helper()
	//nolint:gosec // pkg is a compile-time constant from corePackages, not user input
	out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var deps []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			deps = append(deps, l)
		}
	}
	return deps
}
