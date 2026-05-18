package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortTempDir creates a temp directory under /tmp with a short name so that
// the resulting Unix domain socket path stays under the ~104-byte OS limit.
func shortTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestDaemonRefusesWhenLiveDaemonOnSocket verifies that a second RunDaemon call
// against a home directory where a daemon is already running returns the
// "already running" error instead of orphaning the first daemon.
func TestDaemonRefusesWhenLiveDaemonOnSocket(t *testing.T) {
	homeDir := shortTempDir(t, "rlsh-live-")
	sockPath := filepath.Join(homeDir, ".rallish", "rallish.sock")

	shutdown1 := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunDaemon(context.Background(), homeDir, shutdown1)
	}()

	// Poll until the socket file appears (max 2 s).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon socket to appear")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Also wait until the socket is actually dialable (file may appear before
	// Listen is ready on slow CI).
	deadline2 := time.Now().Add(2 * time.Second)
	for dialExistingDaemon(sockPath, 200*time.Millisecond) != nil {
		if time.Now().After(deadline2) {
			t.Fatal("timed out waiting for daemon to become dialable")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Second attempt must fail with the "already running" message.
	shutdown2 := make(chan struct{})
	err := RunDaemon(context.Background(), homeDir, shutdown2)
	if err == nil {
		t.Fatal("expected error from second RunDaemon, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected 'already running' in error, got: %v", err)
	}

	// Shut down the first daemon and wait for it to exit cleanly.
	close(shutdown1)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first daemon exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first daemon to stop")
	}

	// Socket file should be gone after the first daemon cleans up.
	if _, err := os.Stat(sockPath); err == nil {
		t.Errorf("socket file still exists after daemon shutdown: %s", sockPath)
	}
}

// TestDaemonReclaimsStaleSocket verifies that RunDaemon replaces a stale socket
// file (one that exists on disk but no live daemon is listening) instead of
// refusing to start.
func TestDaemonReclaimsStaleSocket(t *testing.T) {
	homeDir := shortTempDir(t, "rlsh-stale-")
	sockDir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(sockDir, "rallish.sock")

	// Create a plain file (not a real socket) to simulate a stale socket.
	if err := os.WriteFile(sockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	shutdown := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunDaemon(context.Background(), homeDir, shutdown)
	}()

	// Wait until the socket becomes dialable, meaning the daemon reclaimed it.
	deadline := time.Now().Add(2 * time.Second)
	for dialExistingDaemon(sockPath, 200*time.Millisecond) != nil {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon to reclaim stale socket")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Daemon started successfully; shut it down.
	close(shutdown)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon with stale socket reclaim exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for daemon to stop")
	}
}
