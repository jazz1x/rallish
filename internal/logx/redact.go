package logx

import "regexp"

// placeholder replaces a detected secret value in log output. It is intentionally
// distinct and greppable so a leaked-then-redacted value is obvious in a log.
const placeholder = "[REDACTED]"

// secretPattern pairs a precompiled matcher with its replacement template. The
// patterns are deliberately HIGH-PRECISION: each keys off a provider-specific
// prefix, a sensitive key name, or a structural marker (PEM header), so ordinary
// prose ("the token bucket", "secret sauce") is never redacted. False negatives
// are preferred over false positives here — redaction must not corrupt diagnostics.
type secretPattern struct {
	re   *regexp.Regexp
	repl string
}

// secretPatterns is the SSOT list of value-shaped secret matchers applied to log
// output. Order matters only for overlapping matches (none currently overlap).
var secretPatterns = []secretPattern{
	// PEM private-key blocks (any key type), including the newlines between header
	// and footer — collapse the whole block. (?s) makes . span newlines.
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), placeholder},

	// Provider API keys / tokens, each with a distinctive prefix.
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{16,}`), placeholder},    // Anthropic
	{regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`), placeholder},          // OpenAI-style
	{regexp.MustCompile(`gh[posru]_[A-Za-z0-9]{30,}`), placeholder},   // GitHub (ghp_/gho_/ghs_/ghr_/ghu_)
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), placeholder}, // Slack
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), placeholder},             // AWS access key id
	{regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`), placeholder},       // Google API key

	// Authorization: Bearer <token> — require a token-shaped value of real length
	// so "Bearer authentication" prose is not redacted. Keep the scheme word.
	{regexp.MustCompile(`(?i)(bearer )[A-Za-z0-9._~+/=-]{20,}`), `${1}` + placeholder},

	// Sensitive env-style assignment: KEY=value / KEY: value where the key name
	// contains a credential word. Keep the key + separator, redact the value.
	{
		regexp.MustCompile(`(?i)([A-Z0-9_]*(?:API[_-]?KEY|SECRET|TOKEN|PASSWORD|PASSWD|PRIVATE[_-]?KEY|ACCESS[_-]?KEY|CREDENTIAL)[A-Z0-9_]*\s*[=:]\s*"?)([^\s"]{4,})`),
		`${1}` + placeholder,
	},
}

// Redact returns s with any recognized secret values replaced by a placeholder.
// It is the single log-time redaction function: the slog middleware (and any
// other log sink) routes string output through it so a secret that leaks into a
// message or attribute is masked before it is written. Pure and allocation-light
// on the common no-match path (regexp scans, no replacement).
func Redact(s string) string {
	if s == "" {
		return s
	}
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}
