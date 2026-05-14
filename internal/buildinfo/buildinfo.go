// Package buildinfo provides version metadata injected at link time.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

var (
	version = ""
	commit  = ""
	date    = ""
)

// Version returns the build version, falling back to debug.ReadBuildInfo if unset.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return ""
}

// Commit returns the build commit SHA.
func Commit() string {
	return commit
}

// Date returns the build timestamp.
func Date() string {
	return date
}

// String returns a formatted version string.
func String() string {
	v := Version()
	if v == "" {
		v = "unknown"
	}
	return fmt.Sprintf("hocketty %s (commit: %s, built: %s)", v, Commit(), Date())
}
