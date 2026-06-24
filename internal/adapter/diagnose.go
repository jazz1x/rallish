package adapter

import "strings"

// FailureKind categorizes why an adapter subprocess produced no usable
// TurnResponse. It lets callers turn an opaque parse/exit failure into an
// actionable, capability-gated message.
type FailureKind int

// FailureKind values returned by [DiagnoseOutput].
const (
	// FailureUnknown means no recognized signature was found in the output;
	// the caller should keep its own generic error.
	FailureUnknown FailureKind = iota
	// FailureUnauthenticated means the runtime CLI reported a missing or
	// invalid credential (login/API-key problem).
	FailureUnauthenticated
	// FailureRateLimited means the runtime CLI reported a rate or usage limit.
	FailureRateLimited
)

// authMarkers are case-insensitive substrings that signal a credential problem.
// Kept narrow and high-precision: every entry is a phrase a CLI prints only on
// an auth failure, never in a normal turn. Bare HTTP codes like "401" are
// deliberately avoided (they can appear in legitimate response text); the
// multi-word forms below carry the auth context.
var authMarkers = []string{
	"not authenticated",
	"please log in",
	"please run /login",
	"run `claude` to log in",
	"invalid api key",
	"invalid x-api-key",
	"authentication_error",
	"authentication failed",
	"no api key",
	"missing api key",
	"401 unauthorized",
	"unauthorized: ",
	"credit balance is too low",
}

// rateMarkers are case-insensitive substrings that signal a rate/usage limit.
// As with authMarkers, bare codes are paired with context ("429 too many",
// "quota exceeded") so a benign mention cannot trip the classifier.
var rateMarkers = []string{
	"rate limit",
	"rate_limit",
	"rate-limit",
	"429 too many",
	"too many requests",
	"usage limit",
	"overloaded_error",
	"quota exceeded",
	"quota exhausted",
	"out of quota",
}

// DiagnoseOutput inspects combined adapter output (stdout and/or stderr) for
// well-known auth and rate-limit signatures. tool is the runtime name used in
// the returned hint (e.g. "claude", "kimi").
//
// When a signature matches it returns an actionable hint and ok=true; when
// nothing matches it returns ok=false so the caller keeps its generic message.
// Auth signatures take precedence over rate signatures because an unauthenticated
// CLI can also surface a 401 that superficially reads like a limit.
func DiagnoseOutput(tool, text string) (hint string, kind FailureKind, ok bool) {
	lower := strings.ToLower(text)
	for _, m := range authMarkers {
		if strings.Contains(lower, m) {
			return tool + " runtime is not authenticated — run `" + tool +
				"` once interactively to log in (or set its API key), then retry", FailureUnauthenticated, true
		}
	}
	for _, m := range rateMarkers {
		if strings.Contains(lower, m) {
			return tool + " runtime hit a rate or usage limit — wait and retry, or check your plan limits", FailureRateLimited, true
		}
	}
	return "", FailureUnknown, false
}
