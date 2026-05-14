package adapter

import (
	"strings"
	"syscall"
)

// BuildEnv returns environment variables from the current process that match
// the allowlist. Each entry in allowed is either an exact key or a prefix.
func BuildEnv(allowed ...string) []string {
	env := syscall.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		for _, a := range allowed {
			if key == a || strings.HasPrefix(key, a) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
