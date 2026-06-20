package cli

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/ipc"
)

// brokerClient holds a resolved broker URL and HTTP client.
type brokerClient struct {
	URL    string
	Client *http.Client
}

// resolveBrokerClient resolves the broker URL and HTTP client for the given
// home directory. It prefers a Unix socket (if ~/.rallish/socket exists and
// points within sockDir), and falls back to TCP via ~/.rallish/port.
// Unlike the squash path, this function does NOT auto-start the daemon; callers
// that want auto-start should use the squash-specific path in squash.go.
// connectTimeout controls the Timeout on the returned client (0 = 30 s default).
func resolveBrokerClient(homeDir string, connectTimeout time.Duration) (brokerClient, error) {
	if connectTimeout == 0 {
		connectTimeout = 30 * time.Second
	}
	sockDir := filepath.Join(homeDir, ".rallish")
	socketFile := filepath.Join(sockDir, "socket")
	socketPath, readErr := os.ReadFile(socketFile) //nolint:gosec // well-known path
	trimmed := strings.TrimSpace(string(socketPath))
	if readErr == nil && trimmed != "" && socketUnderRoot(trimmed, sockDir) {
		// The socket pointer (and the socket file) survive a non-graceful daemon
		// death (kill -9, OOM, reboot): daemon.go only removes them after a
		// graceful Serve return. Probe the socket before handing back a client,
		// otherwise callers surface a cryptic "connection refused" instead of the
		// clean missing-broker message. On a dead socket, remove the stale pointer
		// and fall through to the port path (which auto-spawns / returns the clean
		// sentinel), mirroring doctor's daemon check.
		if dialExistingDaemon(trimmed, 300*time.Millisecond) == nil {
			client := ipc.HTTPClientOverSocket(trimmed)
			client.Timeout = connectTimeout
			return brokerClient{URL: "http://rallish.local", Client: client}, nil
		}
		_ = os.Remove(socketFile)
	} else if readErr == nil && trimmed != "" {
		slog.Warn("ignoring socket pointer outside rallish home", "path", trimmed)
	}
	portFile := filepath.Join(sockDir, "port")
	port, err := readPortFile(portFile)
	if err != nil {
		return brokerClient{}, fmt.Errorf("broker not running — start it first with: rallish daemon (no socket or port file): %w", err)
	}
	// The port file survives a non-graceful daemon death (kill -9, OOM, reboot):
	// daemon.go only removes it after a graceful httpSrv.Serve return. Probe the
	// port before handing back a client, otherwise callers surface a cryptic
	// "connection refused" instead of the clean missing-broker message. On a dead
	// port, remove the stale file so the next invocation auto-spawns cleanly.
	if !portReachable(port) {
		_ = os.Remove(portFile)
		return brokerClient{}, fmt.Errorf("broker not running — start it first with: rallish daemon (stale port file removed): dial 127.0.0.1:%s: connection refused", port)
	}
	url := "http://127.0.0.1:" + port
	return brokerClient{URL: url, Client: &http.Client{Timeout: connectTimeout}}, nil
}

// portReachable reports whether a TCP listener answers on 127.0.0.1:port within
// a short timeout. A stale port file (left by a non-graceful daemon death)
// reads fine but dials refused, so callers use this to distinguish a live
// broker from a leftover pointer. An empty port is never reachable.
func portReachable(port string) bool {
	if port == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
