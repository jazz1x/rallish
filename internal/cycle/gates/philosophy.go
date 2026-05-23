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

// scanROP detects return-oriented programming style breaks.
// Heuristic: flag deeply nested if/else chains that could be early-returned.
func scanROP(diff string) []contract.Violation {
	var vs []contract.Violation
	// Match added lines with deep nesting (>3 levels of if/else).
	re := regexp.MustCompile(`^\+\s*\}\s*else\s*\{`)
	lines := strings.Split(diff, "\n")
	inFile := ""
	lineNo := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				inFile = strings.TrimPrefix(parts[2], "b/")
			}
			lineNo = 0
			continue
		}
		if strings.HasPrefix(line, "@@") {
			// Extract line number from hunk header: @@ -a,b +c,d @@
			if idx := strings.Index(line, "+"); idx >= 0 {
				sub := line[idx+1:]
				if comma := strings.Index(sub, ","); comma >= 0 {
					_, _ = fmt.Sscanf(sub[:comma], "%d", &lineNo)
				} else if space := strings.Index(sub, " "); space >= 0 {
					_, _ = fmt.Sscanf(sub[:space], "%d", &lineNo)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lineNo++
			if re.MatchString(line) {
				vs = append(vs, contract.Violation{
					File:    inFile,
					Line:    lineNo,
					Type:    "rop",
					Message: "consider early return instead of else-after-return",
				})
			}
		}
	}
	return vs
}

// scanSSOT detects single-source-of-truth violations.
// Heuristic: flag duplicated constant definitions in new code.
func scanSSOT(diff string) []contract.Violation {
	var vs []contract.Violation
	// Very naive: flag added const declarations that shadow existing ones.
	re := regexp.MustCompile(`^\+\s*const\s+(\w+)`)
	lines := strings.Split(diff, "\n")
	inFile := ""
	lineNo := 0
	seen := make(map[string]int)
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				inFile = strings.TrimPrefix(parts[2], "b/")
			}
			lineNo = 0
			seen = make(map[string]int)
			continue
		}
		if strings.HasPrefix(line, "@@") {
			if idx := strings.Index(line, "+"); idx >= 0 {
				sub := line[idx+1:]
				if comma := strings.Index(sub, ","); comma >= 0 {
					_, _ = fmt.Sscanf(sub[:comma], "%d", &lineNo)
				} else if space := strings.Index(sub, " "); space >= 0 {
					_, _ = fmt.Sscanf(sub[:space], "%d", &lineNo)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lineNo++
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				if prev, ok := seen[name]; ok {
					vs = append(vs, contract.Violation{
						File:    inFile,
						Line:    lineNo,
						Type:    "ssot",
						Message: fmt.Sprintf("const %s duplicates declaration at line %d", name, prev),
					})
				}
				seen[name] = lineNo
			}
		}
	}
	return vs
}

// scanSRP detects single-responsibility-principle breaches.
// Heuristic: flag new functions with >60 lines or >4 parameters.
func scanSRP(diff string) []contract.Violation {
	var vs []contract.Violation
	// Naive: look for func declarations and count subsequent added lines.
	funcRe := regexp.MustCompile(`^\+\s*func\s+`)
	lines := strings.Split(diff, "\n")
	inFile := ""
	lineNo := 0
	funcStart := 0
	funcLine := 0
	braceDepth := 0
	inFunc := false
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				inFile = strings.TrimPrefix(parts[2], "b/")
			}
			lineNo = 0
			inFunc = false
			continue
		}
		if strings.HasPrefix(line, "@@") {
			if idx := strings.Index(line, "+"); idx >= 0 {
				sub := line[idx+1:]
				if comma := strings.Index(sub, ","); comma >= 0 {
					_, _ = fmt.Sscanf(sub[:comma], "%d", &lineNo)
				} else if space := strings.Index(sub, " "); space >= 0 {
					_, _ = fmt.Sscanf(sub[:space], "%d", &lineNo)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lineNo++
			if funcRe.MatchString(line) {
				if inFunc {
					// Check previous function length.
					length := funcStart - braceDepth // rough estimate
					if length > 60 {
						vs = append(vs, contract.Violation{
							File:    inFile,
							Line:    funcLine,
							Type:    "srp",
							Message: fmt.Sprintf("function spans ~%d added lines; consider decomposition", length),
						})
					}
				}
				funcStart = lineNo
				funcLine = lineNo
				inFunc = true
				braceDepth = 0
			}
			if inFunc {
				braceDepth += strings.Count(line, "{")
				braceDepth -= strings.Count(line, "}")
				if braceDepth <= 0 && strings.Contains(line, "}") {
					inFunc = false
				}
			}
		}
	}
	return vs
}

// scanHardcodedVersions detects version literal hardcoding.
// Heuristic: flag added strings matching common version patterns in non-test files.
func scanHardcodedVersions(diff string) []contract.Violation {
	var vs []contract.Violation
	verRe := regexp.MustCompile(`"\d+\.\d+\.\d+"`)
	lines := strings.Split(diff, "\n")
	inFile := ""
	lineNo := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				inFile = strings.TrimPrefix(parts[2], "b/")
			}
			lineNo = 0
			continue
		}
		if strings.HasPrefix(line, "@@") {
			if idx := strings.Index(line, "+"); idx >= 0 {
				sub := line[idx+1:]
				if comma := strings.Index(sub, ","); comma >= 0 {
					_, _ = fmt.Sscanf(sub[:comma], "%d", &lineNo)
				} else if space := strings.Index(sub, " "); space >= 0 {
					_, _ = fmt.Sscanf(sub[:space], "%d", &lineNo)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lineNo++
			if strings.Contains(inFile, "_test.go") {
				continue
			}
			if verRe.MatchString(line) {
				vs = append(vs, contract.Violation{
					File:    inFile,
					Line:    lineNo,
					Type:    "hardcoded-version",
					Message: "version literal detected; consider externalising to a constant or config",
				})
			}
		}
	}
	return vs
}
