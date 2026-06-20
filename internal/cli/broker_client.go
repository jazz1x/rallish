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
		client := ipc.HTTPClientOverSocket(trimmed)
		client.Timeout = connectTimeout
		return brokerClient{URL: "http://rallish.local", Client: client}, nil
	}
	if readErr == nil && trimmed != "" {
		slog.Warn("ignoring socket pointer outside rallish home", "path", trimmed)
	}
	portFile := filepath.Join(sockDir, "port")
	port, err := readPortFile(portFile)
	if err != nil {
		return brokerClient{}, fmt.Errorf("broker not running — start it first with: rallish daemon (no socket or port file): %w", err)
	}
	addr := "127.0.0.1:" + port
	// The port file survives a non-graceful daemon death (kill -9, OOM, reboot):
	// daemon.go only removes it after a graceful httpSrv.Serve return. Probe the
	// port before handing back a client, otherwise callers surface a cryptic
	// "connection refused" instead of the clean missing-broker message. On a dead
	// port, remove the stale file so the next invocation auto-spawns cleanly.
	conn, derr := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if derr != nil {
		_ = os.Remove(portFile)
		return brokerClient{}, fmt.Errorf("broker not running — start it first with: rallish daemon (stale port file removed): %w", derr)
	}
	_ = conn.Close()
	url := "http://" + addr
	return brokerClient{URL: url, Client: &http.Client{Timeout: connectTimeout}}, nil
}
