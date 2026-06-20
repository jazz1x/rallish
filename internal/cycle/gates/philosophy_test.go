package gates

import (
	"strconv"
	"strings"
	"testing"
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
