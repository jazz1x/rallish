package gates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// The PhilosophyGate is the harness's own verification surface — "the harness is
// half the score". These tests are a gate SELF-EVAL: they feed SEEDED violations
// (a diff containing a pattern each scanner targets) and assert the scanner
// CATCHES it, then feed a clean diff and assert ZERO false-green. They are a true
// negative control: if a scanner is gutted to a no-op, the seeded cases FAIL.
//
// The scanners are pure functions over a unified-diff string, so this proves
// detection deterministically without shelling out to git.

// seededDiff wraps added lines in a minimal but well-formed unified-diff frame so
// the scanners' file/hunk bookkeeping (diff --git / @@) is exercised too.
func seededDiff(file string, addedLines ...string) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + file + " b/" + file + "\n")
	b.WriteString("--- a/" + file + "\n")
	b.WriteString("+++ b/" + file + "\n")
	b.WriteString("@@ -1,0 +1," + strconv.Itoa(len(addedLines)) + " @@\n")
	for _, l := range addedLines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func TestPhilosophyScannersCatchSeededViolations(t *testing.T) {
	t.Run("ROP else-after-return is caught", func(t *testing.T) {
		// A seeded ROP smell: an `} else {` block that an early return would flatten.
		diff := seededDiff("svc.go",
			"if err != nil {",
			"\treturn err",
			"} else {",
			"\tdoWork()",
			"}",
		)
		vs := scanROP(diff)
		if len(vs) == 0 {
			t.Fatal("scanROP returned no violations on a seeded `} else {` — the gate is a no-op")
		}
		if vs[0].Type != "rop" {
			t.Fatalf("violation type = %q, want rop", vs[0].Type)
		}
		// The scanner attributes the violation to the diffed file (path prefix is
		// the scanner's own concern; what matters is it is non-empty and our file).
		if !strings.Contains(vs[0].File, "svc.go") {
			t.Fatalf("violation file = %q, want it to reference svc.go", vs[0].File)
		}
	})

	t.Run("SSOT duplicate const is caught", func(t *testing.T) {
		// A seeded SSOT smell: the same const name declared twice in one file.
		diff := seededDiff("config.go",
			"const MaxRetries = 3",
			"someOtherLine := 1",
			"const MaxRetries = 5",
		)
		vs := scanSSOT(diff)
		if len(vs) == 0 {
			t.Fatal("scanSSOT returned no violations on a duplicated const — the gate is a no-op")
		}
		if vs[0].Type != "ssot" {
			t.Fatalf("violation type = %q, want ssot", vs[0].Type)
		}
	})

	t.Run("hardcoded version literal is caught", func(t *testing.T) {
		// A seeded SSOT/version smell: a semver literal baked into non-test code.
		// The scanner targets a quoted semver flush against the quotes ("X.Y.Z").
		diff := seededDiff("client.go",
			"endpoint := apiBase",
			`pinnedVersion := "1.2.3"`,
		)
		vs := scanHardcodedVersions(diff)
		if len(vs) == 0 {
			t.Fatal("scanHardcodedVersions returned no violations on a seeded semver literal — the gate is a no-op")
		}
		if vs[0].Type != "hardcoded-version" {
			t.Fatalf("violation type = %q, want hardcoded-version", vs[0].Type)
		}
	})
}

// TestPhilosophyScannersZeroFalseGreen is the false-positive guard: clean,
// idiomatic added code must produce NO violations. A gate that flags everything
// is as useless as one that flags nothing.
func TestPhilosophyScannersZeroFalseGreen(t *testing.T) {
	clean := seededDiff("clean.go",
		"if err != nil {",
		"\treturn err",
		"}",
		"const MaxRetries = 3",
		"doWork()",
		`name := "rallish"`,
	)
	if vs := scanROP(clean); len(vs) != 0 {
		t.Fatalf("scanROP false-green: flagged clean early-return code: %#v", vs)
	}
	if vs := scanSSOT(clean); len(vs) != 0 {
		t.Fatalf("scanSSOT false-green: flagged a single const decl: %#v", vs)
	}
	if vs := scanHardcodedVersions(clean); len(vs) != 0 {
		t.Fatalf("scanHardcodedVersions false-green: flagged version-free code: %#v", vs)
	}
}

// TestPhilosophyScannerSkipsTestFiles guards the documented exemption: version
// literals in _test.go files are intentionally NOT flagged (test fixtures
// legitimately hardcode versions). This is a deliberate carve-out, not a gap.
func TestPhilosophyScannerSkipsTestFiles(t *testing.T) {
	// Use a flush semver literal — the SAME shape the scanner flags in non-test
	// code — so this genuinely tests the _test.go carve-out, not a non-match.
	diff := seededDiff("client_test.go", `want := "1.2.3"`)
	if vs := scanHardcodedVersions(diff); len(vs) != 0 {
		t.Fatalf("scanHardcodedVersions must skip _test.go files, got: %#v", vs)
	}
}

// TestForEachAddedLineSpacedPath guards path attribution for files whose names
// contain spaces. Git emits such paths UNQUOTED in the "diff --git" header
// (e.g. `diff --git a/dir/my file.go b/dir/my file.go`) and appends a trailing
// tab to the "+++ b/<path>" marker. A whitespace split of the header would
// mis-resolve the file to the path's second word ("file.go"); the walker must
// instead recover the FULL path from the "+++ b/" marker. Impact is the
// Violation.File display string only, but a wrong filename misdirects the
// operator, so this pins exact attribution.
func TestForEachAddedLineSpacedPath(t *testing.T) {
	const path = "dir/my file.go"
	// Mirror git's real output: header with the unquoted spaced path on both
	// sides, a "+++ b/<path>" marker with the trailing tab git appends, and a hunk.
	diff := strings.Join([]string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path + "\t",
		"+++ b/" + path + "\t",
		"@@ -1 +1,2 @@",
		" package x",
		"+var A = 1",
	}, "\n")

	var got []string
	forEachAddedLine(diff, nil, func(a addedLine) { got = append(got, a.file) })

	if len(got) != 1 {
		t.Fatalf("added lines = %d, want 1: %#v", len(got), got)
	}
	if got[0] != path {
		t.Fatalf("file = %q, want %q (spaced path must survive intact, not be split on whitespace)", got[0], path)
	}
}

// TestForEachAddedLinePlusPlusContentLine is the regression guard for the
// "+++ b/" mid-hunk corruption: an ADDED content line whose source begins
// "++ b/<path>" renders raw as "+++ b/<path>" — byte-identical to the per-file
// new-file header. The walker must NOT treat such a content line as a header:
// (a) it must still be SCANNED as an added line (the header-only "+++" case used
// to swallow it), and (b) Violation.File must stay the real file, not flip to the
// path embedded in the content. The discriminator is position: a genuine header
// precedes the first "@@"; a "+++ ..." line after a hunk has started is content.
func TestForEachAddedLinePlusPlusContentLine(t *testing.T) {
	// real.go's hunk contains an added line whose CONTENT is "++ b/other.go",
	// which git renders as the raw line "+++ b/other.go" (leading + for "added").
	diff := strings.Join([]string{
		"diff --git a/real.go b/real.go",
		"--- a/real.go",
		"+++ b/real.go",
		"@@ -1,0 +1,2 @@",
		"+package x",
		"+++ b/other.go", // an added content line, NOT a header
	}, "\n")

	type rec struct {
		file string
		text string
	}
	var got []rec
	forEachAddedLine(diff, nil, func(a addedLine) {
		got = append(got, rec{a.file, a.text})
	})

	// Both added lines must be visited, and the file must remain real.go throughout
	// (the "+++ b/other.go" content line must not override it to other.go).
	want := []rec{
		{"real.go", "+package x"},
		{"real.go", "+++ b/other.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("added lines = %d, want %d (the '+++ b/other.go' content line must be scanned, not swallowed as a header): %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v (file must stay real.go; content line must be scanned verbatim)", i, got[i], want[i])
		}
	}
}

// TestScanHardcodedVersionsPlusPlusContentDoesNotDefeatTestCarveOut is the
// end-to-end false-positive guard: the _test.go carve-out in scanHardcodedVersions
// keys off the resolved file name, so a "+++ b/prod.go" content line inside a
// *_test.go hunk must NOT flip `file` to prod.go and thereby unmask a version
// literal the carve-out is meant to ignore. Under the bug this yielded a spurious
// hardcoded-version violation (and could escalate Run() to a self-audit HALT).
func TestScanHardcodedVersionsPlusPlusContentDoesNotDefeatTestCarveOut(t *testing.T) {
	// A *_test.go hunk that legitimately hardcodes a version, with an embedded
	// "+++ b/prod.go" content line (e.g. a diff snippet inside a fixture) before it.
	diff := strings.Join([]string{
		"diff --git a/foo_test.go b/foo_test.go",
		"--- a/foo_test.go",
		"+++ b/foo_test.go",
		"@@ -1,0 +1,2 @@",
		"+++ b/prod.go", // content line inside the test file's hunk
		`+const Version = "1.2.3"`,
	}, "\n")
	if vs := scanHardcodedVersions(diff); len(vs) != 0 {
		t.Fatalf("a '+++ b/prod.go' content line must not flip the file off foo_test.go and defeat the _test.go carve-out, got: %#v", vs)
	}
}

// TestPhilosophyScannerSkipsSpacedTestFile is the false-positive guard for the
// spaced-path fix: the _test.go carve-out in scanHardcodedVersions keys off the
// resolved file name, so it must still hold when that name contains a space.
// (A regression here would FLAG version literals in spaced test fixtures.)
func TestPhilosophyScannerSkipsSpacedTestFile(t *testing.T) {
	const path = "dir/my fixture_test.go"
	diff := strings.Join([]string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path + "\t",
		"+++ b/" + path + "\t",
		"@@ -1 +1,2 @@",
		" package x",
		`+want := "1.2.3"`,
	}, "\n")
	if vs := scanHardcodedVersions(diff); len(vs) != 0 {
		t.Fatalf("scanHardcodedVersions must skip a spaced _test.go file, got: %#v", vs)
	}
}

// TestNewFileFromGitHeader pins the header fallback used before the "+++ b/"
// marker is seen (and for diffs lacking that marker): it must split on " b/",
// not whitespace, so spaced new-file paths are recovered whole.
func TestNewFileFromGitHeader(t *testing.T) {
	cases := map[string]string{
		"diff --git a/svc.go b/svc.go":                 "svc.go",
		"diff --git a/dir/my file.go b/dir/my file.go": "dir/my file.go",
		"diff --git a/pkg/x.go b/pkg/x.go":             "pkg/x.go",
		"diff --git nonsense":                          "", // no " b/" separator
	}
	for header, want := range cases {
		if got := newFileFromGitHeader(header); got != want {
			t.Errorf("newFileFromGitHeader(%q) = %q, want %q", header, got, want)
		}
	}
}

// TestParseHunkStart pins the hunk-header parser shared by every scanner
// (the +c value of "@@ -a,b +c,d @@"), including the comma-less single-line form.
func TestParseHunkStart(t *testing.T) {
	cases := map[string]int{
		"@@ -1,0 +1,5 @@":            1,
		"@@ -10,3 +42,7 @@ func x()": 42,
		"@@ -0,0 +1 @@":              1, // no comma: single added line
		"@@ -5 +5 @@":                5,
		"garbage":                    0, // no '+' -> 0
	}
	for hunk, want := range cases {
		if got := parseHunkStart(hunk); got != want {
			t.Errorf("parseHunkStart(%q) = %d, want %d", hunk, got, want)
		}
	}
}

// TestForEachAddedLineTracksFileAndLine is the regression guard for the de-dup:
// the shared walker must resolve the correct file AND new-file line number across
// MULTIPLE files and MULTIPLE hunks — the exact bookkeeping that was previously
// copy-pasted into each scanner and could drift. It also proves the per-file
// reset (onFileBoundary) fires at each new file.
func TestForEachAddedLineTracksFileAndLine(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/alpha.go b/alpha.go",
		"--- a/alpha.go",
		"+++ b/alpha.go",
		"@@ -1,0 +10,2 @@",
		"+line ten",
		"+line eleven",
		"@@ -20,0 +30,1 @@",
		"+line thirty",
		"diff --git a/beta.go b/beta.go",
		"--- a/beta.go",
		"+++ b/beta.go",
		"@@ -1,0 +1,1 @@",
		"+beta first",
	}, "\n")

	type rec struct {
		file string
		no   int
		text string
	}
	var got []rec
	var boundaries []string
	forEachAddedLine(diff, func(f string) { boundaries = append(boundaries, f) }, func(a addedLine) {
		got = append(got, rec{a.file, a.no, strings.TrimPrefix(a.text, "+")})
	})

	want := []rec{
		{"alpha.go", 10, "line ten"},
		{"alpha.go", 11, "line eleven"},
		{"alpha.go", 30, "line thirty"}, // second hunk resets the line counter to +30
		{"beta.go", 1, "beta first"},    // new file resets to its own hunk start
	}
	if len(got) != len(want) {
		t.Fatalf("added lines = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(boundaries) != 2 || boundaries[0] != "alpha.go" || boundaries[1] != "beta.go" {
		t.Errorf("file boundaries = %v, want [alpha.go beta.go]", boundaries)
	}
}

// TestScanSRPDetectsLongFunctions feeds a diff with a function spanning >60 added
// lines and asserts the SRP scanner flags it.
func TestScanSRPDetectsLongFunctions(t *testing.T) {
	var lines []string
	lines = append(lines, "diff --git a/main.go b/main.go")
	lines = append(lines, "--- a/main.go")
	lines = append(lines, "+++ b/main.go")
	lines = append(lines, "@@ -1,0 +1,67 @@")
	lines = append(lines, "+func longFunc() {")
	for i := 0; i < 65; i++ {
		lines = append(lines, "+	x++")
	}
	lines = append(lines, "+}")
	// scanSRP checks length only when it sees the next function, so add one.
	lines = append(lines, "+func shortFunc() {}")
	diff := strings.Join(lines, "\n")
	vs := scanSRP(diff)
	if len(vs) == 0 {
		t.Fatalf("expected SRP violation for long function, got none")
	}
	if vs[0].Type != "srp" {
		t.Fatalf("violation type = %q, want srp", vs[0].Type)
	}
}

// TestPhilosophyGateDetectsViolations runs PhilosophyGate end-to-end against a real
// temp git repo with seeded ROP and hardcoded-version violations.
func TestPhilosophyGateDetectsViolations(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-phil")

	baselineOut, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() // #nosec G204 -- test helper reading baseline SHA in a temp repo
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	baseline := strings.TrimSpace(string(baselineOut))

	code := `package main
func f(x int) int {
	if x > 0 {
		return 1
	} else {
		return 2
	}
}
var version = "1.2.3"
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "add code")

	state := cycle.State{CycleState: contract.CycleState{ID: "cyc_phil", NextCycleGoal: "feat: test", BaselineSHA: baseline}}
	result, next := PhilosophyGate{}.Run(context.Background(), state)
	warning, ok := result.(contract.GateWarning)
	if !ok {
		t.Fatalf("result = %T, want GateWarning: %#v", result, result.Report())
	}
	if len(warning.R.Violations) == 0 {
		t.Fatalf("expected violations, got none")
	}
	if len(next.ViolationsFound) == 0 {
		t.Fatalf("expected state.ViolationsFound to be set")
	}
}

// TestPhilosophyGateFailsWhenViolationsGrow asserts that philosophy violations become
// a hard failure when the count strictly exceeds the prior cycle's violation count.
func TestPhilosophyGateFailsWhenViolationsGrow(t *testing.T) {
	dir := setupGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feat-phil2")

	baselineOut, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() // #nosec G204 -- test helper reading baseline SHA in a temp repo
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	baseline := strings.TrimSpace(string(baselineOut))

	code := `package main
var version = "1.2.3"
var apiVersion = "4.5.6"
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "add code")

	state := cycle.State{CycleState: contract.CycleState{ID: "cyc_phil2", NextCycleGoal: "feat: test", BaselineSHA: baseline}}
	state.ViolationsFound = []contract.Violation{{Type: "hardcoded-version", Message: "existing"}}
	result, _ := PhilosophyGate{}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure: %#v", result, result.Report())
	}
	if failure.Reason != contract.HaltSelfAuditViolation {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltSelfAuditViolation)
	}
}
