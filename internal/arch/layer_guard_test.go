// Package arch — layer_guard_test.go enforces clean-architecture import
// DIRECTION between rallish's own packages (the inward-only dependency rule),
// complementing import_guard_test.go which forbids specific EXTERNAL deps.
//
// The layering (outer may import inner; never the reverse):
//
//	cmd/rallish                      (composition root — may import anything)
//	  └─ internal/cli                (top consumer layer — wires the CLI)
//	       └─ internal/broker        (server/transport layer)
//	            └─ internal/cycle,   (domain + drivers)
//	               internal/adapter,
//	               internal/session, …
//	                 └─ pkg/contract (the neutral SSOT leaf — wire types only)
//
// The three invariants asserted below are the load-bearing edges of that rule;
// each is verified empirically (today they all hold) and will FAIL the build if
// a future change inverts a dependency:
//
//  1. pkg/contract (SSOT leaf) imports NO internal package — it is parse-able,
//     vendor-neutral, and safe for any consumer to depend on. An internal import
//     here would make the wire contract drag in server/CLI/driver code.
//  2. internal/cli (top consumer) is imported by NO lower layer — only cmd/ and
//     the arch tests may consume it. A lower layer importing cli would invert the
//     composition root.
//  3. internal/broker is imported by NONE of cycle/adapter/contract — the domain
//     and drivers must not depend on the transport server (broker may consume
//     them, not vice versa).
//
// How to verify the guard actually catches an inversion (manual procedure):
//
//  1. Temporarily add to pkg/contract/doc.go:
//     import _ "github.com/jazz1x/rallish/internal/cli"
//  2. Run: go test ./internal/arch/ -run TestLayerGuard
//  3. It must FAIL naming the offending edge. (Go's own compiler also rejects an
//     actual import CYCLE; this guard catches one-way inversions the compiler
//     permits but the architecture forbids.)
//  4. Revert.
package arch

import (
	"strings"
	"testing"
)

const modulePrefix = "github.com/jazz1x/rallish/"

// layerRule is one inward-only invariant: none of the packages reachable from
// `consumers` (transitively) may import a package whose path begins with
// `forbiddenImport`. Expressed as "X must not be imported by Y" so a violation
// message reads as the inverted edge.
type layerRule struct {
	name            string
	consumers       []string // packages whose transitive closure is scanned
	forbiddenImport string   // module-relative path prefix that must be absent
	reason          string
}

// layerRules is the SSOT for the clean-architecture direction invariants.
// Paths are module-relative (joined with modulePrefix); matched with HasPrefix
// against each transitive dep of every consumer.
var layerRules = []layerRule{
	{
		name:            "contract_is_ssot_leaf",
		consumers:       []string{"pkg/contract"},
		forbiddenImport: "internal/",
		reason:          "pkg/contract is the neutral SSOT leaf — it must import NO internal package (else the wire contract drags in server/CLI/driver code).",
	},
	{
		name:            "cli_is_top_consumer",
		consumers:       []string{"internal/broker", "internal/cycle", "internal/adapter", "internal/session", "internal/router", "pkg/contract"},
		forbiddenImport: "internal/cli",
		reason:          "internal/cli is the top consumer layer — no lower layer may import it (only cmd/ and the arch tests may).",
	},
	{
		name:            "broker_not_imported_by_domain",
		consumers:       []string{"internal/cycle", "internal/adapter", "pkg/contract"},
		forbiddenImport: "internal/broker",
		reason:          "internal/broker is the transport layer — the domain/drivers (cycle/adapter) and the SSOT (contract) must not depend on it.",
	},
}

// importsUnder reports whether dep is the package `forbidden` itself or a
// subpackage of it — WITHOUT false-positiving on a sibling that merely shares a
// textual prefix (e.g. forbidden "internal/cli" must NOT match
// "internal/clipboard"). A trailing-slash forbidden value (e.g. "internal/")
// matches any package beneath that directory.
func importsUnder(dep, forbidden string) bool {
	if strings.HasSuffix(forbidden, "/") {
		return strings.HasPrefix(dep, forbidden)
	}
	return dep == forbidden || strings.HasPrefix(dep, forbidden+"/")
}

// TestLayerGuard asserts every inward-only invariant in layerRules. Each rule
// scans the transitive import closure of its consumers and fails if any of them
// reaches the forbidden inner→outer edge.
func TestLayerGuard(t *testing.T) {
	for _, rule := range layerRules {
		rule := rule
		t.Run(rule.name, func(t *testing.T) {
			forbidden := modulePrefix + rule.forbiddenImport
			for _, consumer := range rule.consumers {
				pkg := modulePrefix + consumer
				deps := listDeps(t, pkg)
				for _, dep := range deps {
					if importsUnder(dep, forbidden) {
						t.Errorf(
							"LAYER VIOLATION: %q transitively imports %q\n  rule: %s",
							pkg, dep, rule.reason,
						)
					}
				}
			}
		})
	}
}

// TestLayerMatcher is a self-test guaranteeing the prefix predicate is NOT a
// no-op: it must catch an inverted edge and must not false-positive on a
// legitimate inward edge or a near-miss path. Mirrors TestForbiddenMatcher.
func TestLayerMatcher(t *testing.T) {
	cases := []struct {
		dep        string
		forbidden  string // module-relative
		wantCaught bool
	}{
		// Inversions that MUST be caught:
		{modulePrefix + "internal/cli", "internal/cli", true},
		{modulePrefix + "internal/cli/addassets", "internal/cli", true}, // subpackage counts
		{modulePrefix + "internal/broker", "internal/broker", true},
		{modulePrefix + "internal/session", "internal/", true}, // any internal from contract
		// Legitimate edges that must NOT be caught:
		{modulePrefix + "pkg/contract", "internal/cli", false},
		{modulePrefix + "internal/cycle", "internal/broker", false}, // cycle != broker
		{"fmt", "internal/", false},
		{"encoding/json", "internal/cli", false},
		// Near-miss: a sibling that shares a prefix segment must not false-positive.
		{modulePrefix + "internal/clipboard", "internal/cli", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.dep, func(t *testing.T) {
			forbidden := modulePrefix + tc.forbidden
			caught := importsUnder(tc.dep, forbidden)
			if caught != tc.wantCaught {
				if tc.wantCaught {
					t.Errorf("layer matcher MISSED inversion: dep %q vs forbidden %q", tc.dep, forbidden)
				} else {
					t.Errorf("layer matcher FALSE-POSITIVE: dep %q vs forbidden %q", tc.dep, forbidden)
				}
			}
		})
	}
}
