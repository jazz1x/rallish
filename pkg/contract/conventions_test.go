package contract_test

import (
	"strings"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// sampleAgentsMD is a representative AGENTS.md excerpt (without frontmatter).
const sampleAgentsMD = `# AGENTS.md — Coding Guidelines

## Build & Test

Run ` + "`make check-all`" + ` before every push.

- Always run tests before committing.
- Use table-driven tests.

## Code Style

- Go 1.25 idioms.
- No fmt.Println in library code.

## Commit Messages

Follow conventional commits.
`

// sampleClaudeMD is a representative CLAUDE.md excerpt.
const sampleClaudeMD = `# Global Discipline

## Response Brevity
- Prefer tables and bullet points over prose walls.

## Branch & Workflow Discipline
- NEVER push directly to main.
- Always feature branch + PR.
`

// sampleAgentsMDWithFrontmatter tests optional YAML frontmatter tolerance.
const sampleAgentsMDWithFrontmatter = `---
version: 1.1
scope: repo
---
# AGENTS.md — With Frontmatter

## Build

- Run make test.
`

func TestParseConventions_BothFiles(t *testing.T) {
	sources := map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMD,
		string(contract.ConventionSourceClaudeMD): sampleClaudeMD,
	}
	c := contract.ParseConventions(sources)

	// Both sources must be present in Sections and Digests.
	for _, src := range []contract.ConventionSource{
		contract.ConventionSourceAgentsMD,
		contract.ConventionSourceClaudeMD,
	} {
		if _, ok := c.Sections[src]; !ok {
			t.Errorf("missing Sections for source %q", src)
		}
		if digest, ok := c.Digests[src]; !ok || digest == "" {
			t.Errorf("missing or empty Digest for source %q", src)
		}
	}
}

func TestParseConventions_AgentsMD_Sections(t *testing.T) {
	c := contract.ParseConventions(map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMD,
	})
	sections := c.Sections[contract.ConventionSourceAgentsMD]

	// Expect at least the headings we put in.
	wantHeadings := []string{"AGENTS.md — Coding Guidelines", "Build & Test", "Code Style", "Commit Messages"}
	found := map[string]bool{}
	for _, s := range sections {
		found[s.Heading] = true
	}
	for _, h := range wantHeadings {
		if !found[h] {
			t.Errorf("expected heading %q not found in parsed sections", h)
		}
	}
}

func TestParseConventions_AgentsMD_Directives(t *testing.T) {
	c := contract.ParseConventions(map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMD,
	})
	sections := c.Sections[contract.ConventionSourceAgentsMD]

	var buildSection *contract.ConventionSection
	for i := range sections {
		if sections[i].Heading == "Build & Test" {
			buildSection = &sections[i]
			break
		}
	}
	if buildSection == nil {
		t.Fatal("Build & Test section not found")
	}
	want := []string{"Always run tests before committing.", "Use table-driven tests."}
	if len(buildSection.Directives) != len(want) {
		t.Errorf("expected %d directives, got %d: %v", len(want), len(buildSection.Directives), buildSection.Directives)
	}
	for i, w := range want {
		if i < len(buildSection.Directives) && buildSection.Directives[i] != w {
			t.Errorf("directive[%d]: got %q, want %q", i, buildSection.Directives[i], w)
		}
	}
}

func TestParseConventions_Frontmatter_Tolerated(t *testing.T) {
	c := contract.ParseConventions(map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMDWithFrontmatter,
	})
	sections := c.Sections[contract.ConventionSourceAgentsMD]

	// The "---" frontmatter block must not appear as a heading or directive.
	for _, s := range sections {
		if strings.Contains(s.Heading, "---") {
			t.Errorf("frontmatter leaked into heading: %q", s.Heading)
		}
		if strings.Contains(s.Heading, "version:") || strings.Contains(s.Heading, "scope:") {
			t.Errorf("frontmatter field leaked into heading: %q", s.Heading)
		}
	}
	// The real heading and directive must be present.
	found := false
	for _, s := range sections {
		if s.Heading == "Build" {
			found = true
			if len(s.Directives) == 0 || s.Directives[0] != "Run make test." {
				t.Errorf("Build directive wrong: %v", s.Directives)
			}
		}
	}
	if !found {
		t.Error("Build heading not found after stripping frontmatter")
	}
}

func TestParseConventions_MissingSource_Empty_NoError(t *testing.T) {
	// Empty string value = file absent: parse-don't-validate, no error, no sections.
	c := contract.ParseConventions(map[string]string{
		string(contract.ConventionSourceAgentsMD): "",
		string(contract.ConventionSourceClaudeMD): "",
	})
	if len(c.Sections) != 0 {
		t.Errorf("expected 0 sections for absent files, got %d", len(c.Sections))
	}
	if len(c.Digests) != 0 {
		t.Errorf("expected 0 digests for absent files, got %d", len(c.Digests))
	}
}

func TestParseConventions_NilSources_NoError(t *testing.T) {
	// nil map = no sources at all.
	c := contract.ParseConventions(nil)
	if len(c.Sections) != 0 {
		t.Errorf("expected 0 sections for nil sources, got %d", len(c.Sections))
	}
}

func TestParseConventions_DigestDeterministic(t *testing.T) {
	// Same content must produce the same digest every call.
	c1 := contract.ParseConventions(map[string]string{string(contract.ConventionSourceAgentsMD): sampleAgentsMD})
	c2 := contract.ParseConventions(map[string]string{string(contract.ConventionSourceAgentsMD): sampleAgentsMD})
	d1 := c1.Digests[contract.ConventionSourceAgentsMD]
	d2 := c2.Digests[contract.ConventionSourceAgentsMD]
	if d1 != d2 {
		t.Errorf("digest not deterministic: %q vs %q", d1, d2)
	}
}

func TestParseConventions_DigestChangesOnContentChange(t *testing.T) {
	c1 := contract.ParseConventions(map[string]string{string(contract.ConventionSourceAgentsMD): sampleAgentsMD})
	c2 := contract.ParseConventions(map[string]string{string(contract.ConventionSourceAgentsMD): sampleAgentsMD + "\n# Extra Section\n"})
	d1 := c1.Digests[contract.ConventionSourceAgentsMD]
	d2 := c2.Digests[contract.ConventionSourceAgentsMD]
	if d1 == d2 {
		t.Error("digest must differ when content differs")
	}
}

func TestNewConventionsLoadedEntry_Basic(t *testing.T) {
	c := contract.ParseConventions(map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMD,
	})
	entry := contract.NewConventionsLoadedEntry(1000, "cycle-1", c)

	if entry.Type != contract.LedgerEventConventionsLoaded {
		t.Errorf("unexpected event type: %q", entry.Type)
	}
	if entry.CycleID != "cycle-1" {
		t.Errorf("unexpected cycle id: %q", entry.CycleID)
	}
	if !strings.Contains(entry.Summary, "AGENTS.md") {
		t.Errorf("summary must mention loaded source: %q", entry.Summary)
	}
	// Files slot must carry the digest mapping.
	if len(entry.Files) == 0 {
		t.Error("expected digest entry in Files field")
	}
	found := false
	for _, f := range entry.Files {
		if strings.HasPrefix(f, "AGENTS.md=") {
			found = true
		}
	}
	if !found {
		t.Errorf("AGENTS.md digest not in Files: %v", entry.Files)
	}
}

func TestNewConventionsLoadedEntry_NoSources(t *testing.T) {
	c := contract.ParseConventions(nil)
	entry := contract.NewConventionsLoadedEntry(1000, "cycle-1", c)

	if entry.Type != contract.LedgerEventConventionsLoaded {
		t.Errorf("unexpected event type: %q", entry.Type)
	}
	if !strings.Contains(entry.Summary, "none") {
		t.Errorf("summary must indicate none: %q", entry.Summary)
	}
}

// TestParseConventions_FalsePositiveGuard is the false-positive guard:
// a normal repo with both AGENTS.md and CLAUDE.md present must load cleanly
// with no sections missing or digests empty. This guards against the parser
// halting or returning garbage on well-formed input.
func TestParseConventions_FalsePositiveGuard(t *testing.T) {
	sources := map[string]string{
		string(contract.ConventionSourceAgentsMD): sampleAgentsMD,
		string(contract.ConventionSourceClaudeMD): sampleClaudeMD,
	}
	c := contract.ParseConventions(sources)

	if len(c.Sections) == 0 {
		t.Error("false-positive guard: expected non-empty sections for a normal repo")
	}
	for src, sections := range c.Sections {
		if len(sections) == 0 {
			t.Errorf("false-positive guard: source %q has 0 sections (expected >0)", src)
		}
	}
	for src, digest := range c.Digests {
		if len(digest) != 64 { // SHA-256 hex = 64 chars
			t.Errorf("false-positive guard: source %q digest malformed: %q", src, digest)
		}
	}
}
