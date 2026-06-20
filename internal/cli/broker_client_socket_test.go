//go:build !windows

package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/ipc"
)

// shortHome returns a short-named temp home dir (and its ~/.rallish subdir).
// A short path matters because the live-socket test binds a unix socket under
// it, and sockaddr_un.sun_path is capped at ~104 bytes on macOS — the long
// names t.TempDir() embeds would overflow it with "bind: invalid argument".
func shortHome(t *testing.T) (homeDir, rallishDir string) {
	t.Helper()
	homeDir, err := os.MkdirTemp("", "rl")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })
	rallishDir = filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(rallishDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return homeDir, rallishDir
}

// TestResolveBrokerClientStaleSocketFile verifies that a socket pointer left
// behind by a non-graceful daemon death (kill -9, OOM, reboot) — present and
// in-root but with nothing listening — is detected as dead, yields the clean
// "broker not running" sentinel instead of a cryptic "connection refused"
// surfaced later by the caller, and is removed so the next invocation
// auto-spawns cleanly. This is the default macOS/Linux path and was previously
// unprobed.
func TestResolveBrokerClientStaleSocketFile(t *testing.T) {
	homeDir, dir := shortHome(t)

	// In-root socket pointer with no listener → stale.
	sockPath := filepath.Join(dir, "rallish.sock")
	socketFile := filepath.Join(dir, "socket")
	if err := os.WriteFile(socketFile, []byte(sockPath), 0o600); err != nil {
		t.Fatalf("write socket pointer: %v", err)
	}
	// A stale port file commonly survives the same crash; it must also be
	// removed so the path collapses to the clean sentinel.
	portFile := filepath.Join(dir, "port")
	if err := os.WriteFile(portFile, []byte(freeDeadPort(t)), 0o600); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	_, err := resolveBrokerClient(homeDir, time.Second)
	if err == nil {
		t.Fatal("expected error for stale socket, got nil")
	}
	if !strings.Contains(err.Error(), "broker not running") {
		t.Fatalf("error should be the clean sentinel, got: %v", err)
	}
	if _, statErr := os.Stat(socketFile); !os.IsNotExist(statErr) {
		t.Fatalf("stale socket pointer should have been removed, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(portFile); !os.IsNotExist(statErr) {
		t.Fatalf("stale port file should have been removed, stat err = %v", statErr)
	}
}

// TestResolveBrokerClientLiveSocketFile is the false-positive guard: a socket
// pointer referencing a LIVE unix-socket listener must still resolve to the
// socket URL and must NOT be removed, so the liveness probe does not regress
// the happy path.
func TestResolveBrokerClientLiveSocketFile(t *testing.T) {
	homeDir, dir := shortHome(t)

	sockPath := filepath.Join(dir, "rallish.sock")
	s := ipc.Socket{Path: sockPath}
	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	socketFile := filepath.Join(dir, "socket")
	if err := os.WriteFile(socketFile, []byte(sockPath), 0o600); err != nil {
		t.Fatalf("write socket pointer: %v", err)
	}

	bc, err := resolveBrokerClient(homeDir, time.Second)
	if err != nil {
		t.Fatalf("live socket should resolve, got: %v", err)
	}
	if bc.URL != "http://rallish.local" {
		t.Fatalf("URL = %q want http://rallish.local", bc.URL)
	}
	if _, statErr := os.Stat(socketFile); statErr != nil {
		t.Fatalf("live socket pointer must NOT be removed, stat err = %v", statErr)
	}
}
