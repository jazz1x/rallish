package logx

import (
	"strings"
	"testing"
)

func TestRedact_MasksSecrets(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		mustGo string // substring that must NOT survive
	}{
		{"anthropic", "key=sk-ant-api03-abcdef0123456789ABCDEF", "sk-ant-api03"},
		{"openai", "OPENAI=sk-abcdefghijklmnopqrstuvwxyz0123", "sk-abcdefghij"},
		{"github", "token ghp_abcdefghijklmnopqrstuvwxyz0123456789", "ghp_abcdef"},
		{"slack", "xoxb-123456789012-abcdefABCDEF", "xoxb-123456789012"},
		{"aws akid", "id AKIAIOSFODNN7EXAMPLE here", "AKIAIOSFODNN7EXAMPLE"},
		{"bearer", "Authorization: Bearer abcdef0123456789ABCDEF01", "abcdef0123456789ABCDEF01"},
		{"env api key", "ANTHROPIC_API_KEY=supersecretvalue123", "supersecretvalue123"},
		{"env token colon", "GITHUB_TOKEN: ghxyzsecretvalue99", "ghxyzsecretvalue99"},
		{"env password", "DB_PASSWORD=hunter2hunter2", "hunter2hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			if strings.Contains(got, tc.mustGo) {
				t.Fatalf("secret survived redaction: in=%q got=%q (still contains %q)", tc.in, got, tc.mustGo)
			}
			if !strings.Contains(got, placeholder) {
				t.Fatalf("expected %q in %q", placeholder, got)
			}
		})
	}
}

func TestRedact_PreservesKeyName(t *testing.T) {
	got := Redact("ANTHROPIC_API_KEY=supersecretvalue123")
	if !strings.HasPrefix(got, "ANTHROPIC_API_KEY=") {
		t.Fatalf("env redaction must keep the key name: %q", got)
	}
}

func TestRedact_PEMBlock(t *testing.T) {
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\nMIIBdeadbeef==\nmorebytes==\n-----END OPENSSH PRIVATE KEY-----"
	got := Redact("here is the key:\n" + pem + "\ndone")
	if strings.Contains(got, "deadbeef") || strings.Contains(got, "BEGIN OPENSSH") {
		t.Fatalf("PEM block survived: %q", got)
	}
	if !strings.Contains(got, placeholder) {
		t.Fatalf("expected placeholder, got %q", got)
	}
}

// False-positive guard: ordinary prose and benign values must pass through
// unchanged. Redaction that corrupts diagnostics is worse than a missed token.
func TestRedact_NoFalsePositives(t *testing.T) {
	benign := []string{
		"the token bucket refills every second",
		"secret sauce in the recipe",
		"password reset email sent",
		"Bearer authentication is supported", // no token-shaped value follows
		"git push to origin main succeeded",
		"running go test ./... with -race",
		"sk-", // too short / no body
		"committed 42 files, hash abc123",
	}
	for _, s := range benign {
		if got := Redact(s); got != s {
			t.Errorf("benign string was altered:\n in=%q\nout=%q", s, got)
		}
	}
}

func TestRedact_Empty(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Fatalf("empty should stay empty, got %q", got)
	}
}
