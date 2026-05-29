package buildinfo

import (
	"strings"
	"testing"
)

func TestVersion(_ *testing.T) {
	// When not injected at link time, Version may return empty or the module version.
	// This call must not panic.
	_ = Version()
}

func TestCommit(_ *testing.T) {
	_ = Commit()
}

func TestDate(_ *testing.T) {
	_ = Date()
}

func TestString(t *testing.T) {
	s := String()
	if !strings.HasPrefix(s, "rallish ") {
		t.Fatalf("expected prefix 'rallish ', got %q", s)
	}
	if !strings.Contains(s, "commit:") {
		t.Fatalf("expected 'commit:' in version string, got %q", s)
	}
	if !strings.Contains(s, "built:") {
		t.Fatalf("expected 'built:' in version string, got %q", s)
	}
}
