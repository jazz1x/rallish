package contract

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ConventionSource identifies a known convention-file format by its canonical
// file name.
type ConventionSource string

const (
	// ConventionSourceAgentsMD is the OpenAI/Anthropic AGENTS.md convention file.
	ConventionSourceAgentsMD ConventionSource = "AGENTS.md"
	// ConventionSourceClaudeMD is the Claude Code CLAUDE.md convention file.
	ConventionSourceClaudeMD ConventionSource = "CLAUDE.md"
)

// ConventionSection is one heading-delimited block from a convention file.
// The Heading is the markdown heading text (trimmed, without the # prefix).
// Directives are the bullet-list items under that heading (trimmed, without
// the leading - or * marker). Other content (code blocks, tables, prose) is
// kept as-is in RawLines so no information is silently discarded.
//
// Design note: this is a structural parse, not an NL enforcer. It captures
// the document shape (sections + bullets); whether a directive is "obeyed" is
// a human or verifier concern — NOT auto-enforceable by rallish in general.
// Auto-enforcement of arbitrary natural-language rules is explicitly DEFERRED
// because NL rules are not machine-checkable in general.
type ConventionSection struct {
	// Heading is the ATX heading text (without the leading '#' markers), trimmed.
	Heading string
	// Directives are bullet-list items under this section.
	Directives []string
	// RawLines preserves every raw line of the section body (after the heading)
	// so callers can inspect code blocks, tables, or prose without data loss.
	RawLines []string
}

// Conventions is the structured output of ParseConventions.
// It maps each source file name to its parsed sections.
// A missing or empty source yields an empty slice (NOT an error).
type Conventions struct {
	// Sections maps ConventionSource → ordered sections parsed from that file.
	Sections map[ConventionSource][]ConventionSection
	// Digests maps ConventionSource → hex SHA-256 of the raw file content.
	// Used for ledger recording (tamper-evident reference) without storing the
	// full content in the ledger entry.
	Digests map[ConventionSource]string
}

// ParseConventions parses AGENTS.md-style and CLAUDE.md-style convention files
// into a structured Conventions value. It is the SSOT for convention ingestion.
//
// sources maps a ConventionSource key to the raw file content string. A key
// with an empty string value is treated as "file absent" and yields zero sections
// for that source — absence is not an error (both files are optional).
//
// Parse-don't-validate: the parser structures whatever is parseable. A
// malformed or unexpected section does not halt parsing; it is captured in
// RawLines with no Directives. Both files are plain markdown; AGENTS.md v1.1
// allows optional YAML frontmatter — the parser skips a leading --- / ---
// frontmatter block transparently.
//
// This function reads STRUCTURE only — it does NOT evaluate or enforce
// any directive. NL-rule enforcement is explicitly DEFERRED.
func ParseConventions(sources map[string]string) Conventions {
	c := Conventions{
		Sections: make(map[ConventionSource][]ConventionSection),
		Digests:  make(map[ConventionSource]string),
	}
	for rawKey, content := range sources {
		key := ConventionSource(rawKey)
		if content == "" {
			continue
		}
		sum := sha256.Sum256([]byte(content))
		c.Digests[key] = hex.EncodeToString(sum[:])
		c.Sections[key] = parseSections(content)
	}
	return c
}

// parseSections splits markdown content into heading-delimited ConventionSections.
// It tolerates optional YAML frontmatter (--- ... ---) at the top of the file.
func parseSections(content string) []ConventionSection {
	scanner := bufio.NewScanner(strings.NewReader(content))

	// Skip optional YAML frontmatter: a leading line of exactly "---" opens the
	// block; the next "---" closes it. Only applies when the very first non-blank
	// line is "---".
	var lines []string
	inFrontmatter := false
	frontmatterDone := false
	firstContentLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstContentLine && !frontmatterDone {
			trimmed := strings.TrimSpace(line)
			if trimmed == "---" {
				inFrontmatter = true
				firstContentLine = false
				continue
			}
			firstContentLine = false
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				frontmatterDone = true
			}
			continue
		}
		lines = append(lines, line)
	}

	var sections []ConventionSection
	var current *ConventionSection

	flush := func() {
		if current != nil {
			sections = append(sections, *current)
			current = nil
		}
	}

	for _, line := range lines {
		if isATXHeading(line) {
			flush()
			heading := parseHeading(line)
			current = &ConventionSection{Heading: heading}
			continue
		}
		if current == nil {
			// Lines before the first heading: create an implicit root section.
			current = &ConventionSection{Heading: ""}
		}
		current.RawLines = append(current.RawLines, line)
		if directive, ok := parseBullet(line); ok {
			current.Directives = append(current.Directives, directive)
		}
	}
	flush()
	return sections
}

// isATXHeading reports whether line is an ATX markdown heading (# / ## / ...).
func isATXHeading(line string) bool {
	trimmed := strings.TrimLeft(line, "#")
	hashes := len(line) - len(trimmed)
	return hashes >= 1 && hashes <= 6 && (len(trimmed) == 0 || trimmed[0] == ' ')
}

// parseHeading extracts the heading text from an ATX heading line.
func parseHeading(line string) string {
	trimmed := strings.TrimLeft(line, "#")
	return strings.TrimSpace(trimmed)
}

// parseBullet extracts the directive text from a markdown bullet list item.
// Recognises "- text", "* text", and "+ text" bullets. Returns ("", false) for
// non-bullet lines.
func parseBullet(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 {
		return "", false
	}
	if (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
		return strings.TrimSpace(trimmed[2:]), true
	}
	return "", false
}

// LedgerEventConventionsLoaded records that AGENTS.md/CLAUDE.md convention
// files were ingested for a cycle. Like all G6/G4 decision events, this is a
// RECORD only — it does NOT imply rallish enforces the conventions. Sources is
// the list of file names loaded; Digest is the SHA-256 of each file's content
// (same ChainHash-family function; hex-encoded).
const LedgerEventConventionsLoaded LedgerEventType = "conventions_loaded"

// NewConventionsLoadedEntry builds a ledger entry recording which convention
// files were ingested and their content digests. The digest is a compact,
// tamper-evident reference: a downstream reader can re-hash the file and
// detect drift without storing the full content in the ledger.
//
// If conventions is empty (no files found), the entry still records that
// ingestion ran — absence-of-conventions is itself an auditable fact.
func NewConventionsLoadedEntry(at int64, cycleID string, conventions Conventions) HarnessLedgerEntry {
	var sources []string
	var digestParts []string
	for src, digest := range conventions.Digests {
		sources = append(sources, string(src))
		digestParts = append(digestParts, string(src)+"="+digest)
	}
	summary := "conventions loaded: " + strings.Join(sources, ", ")
	if len(sources) == 0 {
		summary = "conventions loaded: none (no convention files present)"
	}
	entry := NewHarnessLedgerEntry(at, cycleID, LedgerEventConventionsLoaded, summary, nil)
	// Store digest mapping in Files (reusing the existing slot as key=value pairs)
	// to avoid adding a new field to HarnessLedgerEntry. This keeps the schema
	// additive and the ledger format stable.
	entry.Files = digestParts
	return entry
}
