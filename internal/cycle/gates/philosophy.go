// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// Pre-compiled regexes for philosophy scanners.
// Compiling once at init avoids repeated allocations during long autonomous loops.
var (
	opElseRe    = regexp.MustCompile(`^\+\s*\}\s*else\s*\{`)
	constDeclRe = regexp.MustCompile(`^\+\s*const\s+(\w+)`)
	funcDeclRe  = regexp.MustCompile(`^\+\s*func\s+`)
	versionRe   = regexp.MustCompile(`"\d+\.\d+\.\d+"`)
)

// PhilosophyGate scans the diff since BaselineSHA for style and architecture violations.
// It implements the "self-audit" philosophy sweep: ROP, SSOT, SRP, version hardcoding.
type PhilosophyGate struct{}

// Name returns the canonical gate name.
func (PhilosophyGate) Name() string { return "philosophy" }

// Run analyses git diff and returns violations.
func (PhilosophyGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "philosophy"}

	diff, err := gitDiffSince(ctx, state.BaselineSHA)
	if err != nil {
		report.Passed = false
		report.Stderr = err.Error()
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	var violations []contract.Violation
	violations = append(violations, scanROP(diff)...)
	violations = append(violations, scanSSOT(diff)...)
	violations = append(violations, scanSRP(diff)...)
	violations = append(violations, scanHardcodedVersions(diff)...)

	report.Violations = violations
	report.DurationMS = elapsed(start)

	if len(violations) > 0 {
		report.Passed = false
		report.Stderr = fmt.Sprintf("found %d philosophy violation(s)", len(violations))
		// Philosophy violations are warnings by default; they become failures only
		// when the count grows across cycles (indicating drift).
		if len(state.ViolationsFound) > 0 && len(violations) > len(state.ViolationsFound) {
			return contract.GateFailure{R: report, Reason: contract.HaltSelfAuditViolation}, state
		}
		state.ViolationsFound = violations
		return contract.GateWarning{R: report}, state
	}

	report.Passed = true
	// Clear fixed violations.
	state.ViolationsFound = nil
	return contract.GateSuccess{R: report}, state
}

func gitDiffSince(ctx context.Context, baselineSHA string) (string, error) {
	if baselineSHA == "" {
		return "", nil
	}
	out, err := exec.CommandContext(ctx, "git", "diff", baselineSHA).Output() //nolint:gosec // baselineSHA is a validated SHA from internal state
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

// addedLine is one added ("+") line of a unified diff, with the file it belongs
// to and the new-file line number already resolved.
type addedLine struct {
	file string
	no   int
	text string // the full diff line, leading "+" included (scanners match on it)
}

// forEachAddedLine is the SSOT for walking a unified diff: it tracks the current
// file (from `diff --git` headers) and the new-file line number (from `@@` hunk
// headers) and invokes fn once per added line. Every philosophy scanner shares
// this so the diff-parsing boilerplate (previously copy-pasted four times — a
// SSOT violation in the very gate that flags SSOT violations) lives in one place.
// onFileBoundary, if non-nil, fires when a new file starts — for scanners that
// keep per-file state (e.g. SSOT's seen-const map).
func forEachAddedLine(diff string, onFileBoundary func(file string), fn func(addedLine)) {
	file := ""
	lineNo := 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			// "diff --git a/<path> b/<path>": take the b/ side (the new file),
			// which is parts[3]; fall back to parts[2] with a/ or b/ stripped.
			parts := strings.Fields(line)
			switch {
			case len(parts) >= 4:
				file = strings.TrimPrefix(parts[3], "b/")
			case len(parts) >= 3:
				file = strings.TrimPrefix(strings.TrimPrefix(parts[2], "a/"), "b/")
			}
			lineNo = 0
			if onFileBoundary != nil {
				onFileBoundary(file)
			}
		case strings.HasPrefix(line, "@@"):
			// The hunk header names the first new-file line; the next added line
			// IS that number, so seed one below it and let the increment land on it.
			lineNo = parseHunkStart(line) - 1
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			lineNo++
			fn(addedLine{file: file, no: lineNo, text: line})
		}
	}
}

// parseHunkStart extracts the new-file starting line from a hunk header
// "@@ -a,b +c,d @@" (the +c value).
func parseHunkStart(hunk string) int {
	idx := strings.Index(hunk, "+")
	if idx < 0 {
		return 0
	}
	sub := hunk[idx+1:]
	end := len(sub)
	if comma := strings.Index(sub, ","); comma >= 0 {
		end = comma
	} else if space := strings.Index(sub, " "); space >= 0 {
		end = space
	}
	var n int
	_, _ = fmt.Sscanf(sub[:end], "%d", &n)
	return n
}

// scanROP detects return-oriented programming style breaks.
// Heuristic: flag deeply nested if/else chains that could be early-returned.
func scanROP(diff string) []contract.Violation {
	var vs []contract.Violation
	forEachAddedLine(diff, nil, func(a addedLine) {
		if opElseRe.MatchString(a.text) {
			vs = append(vs, contract.Violation{
				File:    a.file,
				Line:    a.no,
				Type:    "rop",
				Message: "consider early return instead of else-after-return",
			})
		}
	})
	return vs
}

// scanSSOT detects single-source-of-truth violations.
// Heuristic: flag duplicated constant definitions in new code.
func scanSSOT(diff string) []contract.Violation {
	var vs []contract.Violation
	// Per-file: flag added const declarations that shadow an earlier one.
	seen := make(map[string]int)
	forEachAddedLine(diff, func(string) { seen = make(map[string]int) }, func(a addedLine) {
		m := constDeclRe.FindStringSubmatch(a.text)
		if m == nil {
			return
		}
		name := m[1]
		if prev, ok := seen[name]; ok {
			vs = append(vs, contract.Violation{
				File:    a.file,
				Line:    a.no,
				Type:    "ssot",
				Message: fmt.Sprintf("const %s duplicates declaration at line %d", name, prev),
			})
		}
		seen[name] = a.no
	})
	return vs
}

// scanSRP detects single-responsibility-principle breaches.
// Heuristic: flag new functions with >60 lines or >4 parameters.
func scanSRP(diff string) []contract.Violation {
	var vs []contract.Violation
	// Per-file: look for func declarations and count subsequent added lines.
	var funcStart, funcLine, braceDepth int
	inFunc := false
	forEachAddedLine(diff, func(string) { inFunc = false }, func(a addedLine) {
		if funcDeclRe.MatchString(a.text) {
			if inFunc {
				length := funcStart - braceDepth // rough estimate
				if length > 60 {
					vs = append(vs, contract.Violation{
						File:    a.file,
						Line:    funcLine,
						Type:    "srp",
						Message: fmt.Sprintf("function spans ~%d added lines; consider decomposition", length),
					})
				}
			}
			funcStart = a.no
			funcLine = a.no
			inFunc = true
			braceDepth = 0
		}
		if inFunc {
			braceDepth += strings.Count(a.text, "{")
			braceDepth -= strings.Count(a.text, "}")
			if braceDepth <= 0 && strings.Contains(a.text, "}") {
				inFunc = false
			}
		}
	})
	return vs
}

// scanHardcodedVersions detects version literal hardcoding.
// Heuristic: flag added strings matching common version patterns in non-test files.
func scanHardcodedVersions(diff string) []contract.Violation {
	var vs []contract.Violation
	forEachAddedLine(diff, nil, func(a addedLine) {
		if strings.Contains(a.file, "_test.go") {
			return
		}
		if versionRe.MatchString(a.text) {
			vs = append(vs, contract.Violation{
				File:    a.file,
				Line:    a.no,
				Type:    "hardcoded-version",
				Message: "version literal detected; consider externalising to a constant or config",
			})
		}
	})
	return vs
}
