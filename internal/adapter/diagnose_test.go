package adapter

import "testing"

func TestDiagnoseOutput(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantOK   bool
		wantKind FailureKind
	}{
		{"empty", "", false, FailureUnknown},
		{"normal json", `{"self_eval":"confident"}`, false, FailureUnknown},
		{"invalid api key", "Error: Invalid API key provided", true, FailureUnauthenticated},
		{"please login", "Please run /login to continue", true, FailureUnauthenticated},
		{"401", "request failed: 401 Unauthorized", true, FailureUnauthenticated},
		{"credit balance", "Your credit balance is too low to run this", true, FailureUnauthenticated},
		{"rate limit", "Error: rate limit exceeded, retry later", true, FailureRateLimited},
		{"429", "HTTP 429 too many requests from upstream", true, FailureRateLimited},
		{"usage limit", "Claude usage limit reached for this period", true, FailureRateLimited},
		{"case insensitive", "INVALID API KEY", true, FailureUnauthenticated},
		// Benign mentions of bare codes/words must NOT trip the classifier.
		{"benign 401 in text", "logged event 401 to the audit trail", false, FailureUnknown},
		{"benign quota word", "disk quota was increased successfully", false, FailureUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hint, kind, ok := DiagnoseOutput("claude", tc.text)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (text=%q)", ok, tc.wantOK, tc.text)
			}
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", kind, tc.wantKind)
			}
			if ok && hint == "" {
				t.Fatalf("ok=true but empty hint")
			}
			if !ok && hint != "" {
				t.Fatalf("ok=false but non-empty hint %q", hint)
			}
		})
	}
}

// Auth precedence: a string carrying both a 401 (auth) and "rate limit" markers
// must classify as unauthenticated — a 401 is the root cause, the limit is noise.
func TestDiagnoseOutputAuthPrecedence(t *testing.T) {
	_, kind, ok := DiagnoseOutput("claude", "401 unauthorized; also rate limit mentioned")
	if !ok || kind != FailureUnauthenticated {
		t.Fatalf("kind = %v ok = %v, want unauthenticated", kind, ok)
	}
}

func TestDiagnoseOutputToolNameInHint(t *testing.T) {
	hint, _, _ := DiagnoseOutput("kimi", "invalid api key")
	if hint == "" || !contains(hint, "kimi") {
		t.Fatalf("hint %q should name the tool", hint)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
