package cli

import (
	"fmt"
	"log/slog"
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
		return brokerClient{}, fmt.Errorf("broker not running (no socket or port file): %w", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%s", strings.TrimSpace(port))
	return brokerClient{URL: url, Client: &http.Client{Timeout: connectTimeout}}, nil
}
