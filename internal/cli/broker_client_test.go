package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freeDeadPort binds a TCP listener to obtain an unused port, then closes it so
// the port is dead — simulating a port file left behind by a non-graceful
// daemon death (kill -9, OOM, reboot) where daemon.go never reached its cleanup.
func freeDeadPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return strconv.Itoa(port)
}

// TestResolveBrokerClientStalePortFile verifies that a port file pointing at a
// dead port yields the clean "broker not running" sentinel (not a cryptic
// "connection refused" surfaced later by the caller) AND that the stale file is
// removed so the next invocation auto-spawns cleanly.
func TestResolveBrokerClientStalePortFile(t *testing.T) {
	homeDir := t.TempDir()
	dir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	portFile := filepath.Join(dir, "port")
	if err := os.WriteFile(portFile, []byte(freeDeadPort(t)), 0o600); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	_, err := resolveBrokerClient(homeDir, time.Second)
	if err == nil {
		t.Fatal("expected error for stale port file, got nil")
	}
	if !strings.Contains(err.Error(), "broker not running") {
		t.Fatalf("error should be the clean sentinel, got: %v", err)
	}
	if !strings.Contains(err.Error(), "stale port file removed") {
		t.Fatalf("error should note stale removal, got: %v", err)
	}
	if _, statErr := os.Stat(portFile); !os.IsNotExist(statErr) {
		t.Fatalf("stale port file should have been removed, stat err = %v", statErr)
	}
}

// TestResolveBrokerClientLivePortFile is the false-positive guard: a port file
// pointing at a LIVE listener must still resolve successfully and must NOT be
// removed, so the liveness probe does not regress the happy path.
func TestResolveBrokerClientLivePortFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	idx := strings.LastIndex(srv.URL, ":")
	if idx < 0 {
		t.Fatalf("unexpected server URL: %s", srv.URL)
	}
	port := srv.URL[idx+1:]

	homeDir := t.TempDir()
	dir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	portFile := filepath.Join(dir, "port")
	if err := os.WriteFile(portFile, []byte(port), 0o600); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	bc, err := resolveBrokerClient(homeDir, time.Second)
	if err != nil {
		t.Fatalf("live port should resolve, got: %v", err)
	}
	if bc.URL != "http://127.0.0.1:"+port {
		t.Fatalf("URL = %q want http://127.0.0.1:%s", bc.URL, port)
	}
	if _, statErr := os.Stat(portFile); statErr != nil {
		t.Fatalf("live port file must NOT be removed, stat err = %v", statErr)
	}
}
