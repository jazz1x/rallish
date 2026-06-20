package doctor

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writePort drops a ~/.rallish/port file under homeDir pointing at port.
func writePort(t *testing.T, homeDir, port string) {
	t.Helper()
	dir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "port"), []byte(port), 0o600); err != nil {
		t.Fatalf("write port: %v", err)
	}
}

// TestDaemonCheckStalePort verifies that a port file pointing at a dead port is
// reported StatusFail (stale) rather than a false StatusOK "reachable via port
// fallback" — os.Stat alone cannot tell a live port from a leftover one.
func TestDaemonCheckStalePort(t *testing.T) {
	// Obtain then release a port so it is guaranteed dead.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	homeDir := t.TempDir()
	writePort(t, homeDir, port)

	c := daemonCheck(context.Background(), homeDir)
	if c.Status != StatusFail {
		t.Fatalf("status = %q want %q (detail: %s)", c.Status, StatusFail, c.Detail)
	}
	if !strings.Contains(c.Detail, "not reachable") {
		t.Fatalf("detail should flag unreachable, got: %s", c.Detail)
	}
}

// TestDaemonCheckLivePort is the false-positive guard: a port file pointing at a
// LIVE listener must still report StatusOK "reachable via port fallback".
func TestDaemonCheckLivePort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	idx := strings.LastIndex(srv.URL, ":")
	if idx < 0 {
		t.Fatalf("unexpected server URL: %s", srv.URL)
	}
	homeDir := t.TempDir()
	writePort(t, homeDir, srv.URL[idx+1:])

	c := daemonCheck(context.Background(), homeDir)
	if c.Status != StatusOK {
		t.Fatalf("status = %q want %q (detail: %s)", c.Status, StatusOK, c.Detail)
	}
	if !strings.Contains(c.Detail, "port fallback") {
		t.Fatalf("detail should mention port fallback, got: %s", c.Detail)
	}
}
