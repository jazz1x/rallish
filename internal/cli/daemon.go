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
func RunDaemon(ctx context.Context, homeDir string) error {
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

	go func(shutdownCtx context.Context) { //nolint:gosec // background context is for shutdown timeout
		<-shutdownCtx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:gosec // shutdown timeout
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}(ctx)

	// Unix domain socket for CLI↔Daemon internal control plane.
	socketPath := filepath.Join(sockDir, "rallish.sock")
	unixOK := false
	us := ipc.Socket{Path: socketPath}

	if err := us.Remove(); err != nil {
		slog.Warn("failed to remove stale unix socket", "error", err)
	}

	if unixLn, err := us.Listen(); err == nil {
		unixOK = true
		defer func() {
			if unixOK {
				_ = us.Remove()
			}
		}()

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

	if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

type realClock struct{}

func (r *realClock) Now() time.Time { return time.Now() }
