package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jazz1x/rallish/internal/broker"
	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/ipc"
	"github.com/jazz1x/rallish/internal/session"
)

// RunDaemon starts the broker daemon.
// The caller is responsible for trapping signals and closing the shutdown channel
// to trigger a graceful stop; RunDaemon will then clean up its socket files.
func RunDaemon(ctx context.Context, homeDir string, shutdown <-chan struct{}) error {
	sockDir := filepath.Join(homeDir, ".rallish")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	dir := filepath.Join(sockDir, "sessions")
	clock := &realClock{}
	store, err := session.NewStore(dir, clock)
	if err != nil {
		return fmt.Errorf("create session store: %w", err)
	}

	budgeter := budget.NewBudgeter(clock)
	srv := broker.NewServer(store, budgeter)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	portFile := filepath.Join(sockDir, "port")
	if err := os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0o600); err != nil {
		return fmt.Errorf("write port file: %w", err)
	}

	slog.Info("daemon listening", "addr", listener.Addr().String())

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	go func() { //nolint:gosec // background context is for shutdown timeout
		<-shutdown
		// Interrupt active rally sessions before HTTP shutdown so SSE clients
		// receive the terminal {"closed":true} event and unblock promptly.
		broker.CloseAllRallies()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}()

	// Unix domain socket for CLI↔Daemon internal control plane.
	socketPath := filepath.Join(sockDir, "rallish.sock")
	unixOK := false
	us := ipc.Socket{Path: socketPath}

	if err := us.Remove(); err != nil {
		slog.Warn("failed to remove stale unix socket", "error", err)
	}

	var unixLn net.Listener
	if ln, err := us.Listen(); err == nil {
		unixLn = ln
		unixOK = true
		// Restrict to current user; net.Listen("unix") otherwise honours umask
		// and typically produces 0755.
		if cerr := os.Chmod(socketPath, 0o600); cerr != nil {
			slog.Warn("failed to chmod unix socket", "error", cerr)
		}
		socketFile := filepath.Join(sockDir, "socket")
		if werr := os.WriteFile(socketFile, []byte(socketPath), 0o600); werr != nil {
			slog.Warn("failed to write socket file", "error", werr)
		}

		go func() {
			if serr := httpSrv.Serve(unixLn); serr != nil && serr != http.ErrServerClosed {
				slog.Error("unix socket serve error", "error", serr)
			}
		}()

		slog.Info("daemon listening on unix socket", "path", socketPath)
	} else {
		slog.Warn("unix socket unavailable, falling back to tcp only", "error", err)
	}

	serveErr := httpSrv.Serve(listener)
	if unixOK {
		if unixLn != nil {
			_ = unixLn.Close()
		}
		_ = us.Remove()
		_ = os.Remove(filepath.Join(sockDir, "socket"))
	}
	_ = os.Remove(filepath.Join(sockDir, "port"))
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", serveErr)
	}
	return nil
}

type realClock struct{}

func (r *realClock) Now() time.Time { return time.Now() }
