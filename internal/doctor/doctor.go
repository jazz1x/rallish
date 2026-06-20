// Package doctor checks adapters, paths, and permissions.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/adapter/claude"
	"github.com/jazz1x/rallish/internal/adapter/kimi"
	"github.com/jazz1x/rallish/internal/ipc"
	"github.com/jazz1x/rallish/internal/safepath"
)

type pathReporter interface {
	Path() string
}

// Status enumerates the outcome of a single check.
type Status string

// Status values returned by [Inspect] on each [Check].
const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusInfo Status = "info"
)

// Check is a structured result for a single doctor probe — easier to
// render in a TUI than scraping slog output.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report is the structured result of [Inspect].
type Report struct {
	Checks []Check
}

// Inspect runs all doctor probes and returns a structured report.
// Errors per-probe are surfaced as Status fields on individual checks;
// only a totally fatal condition (e.g. impossible home directory)
// surfaces as the returned error.
func Inspect(ctx context.Context, homeDir string) (Report, error) {
	rep := Report{}
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	}
	if homeDir == "" {
		return rep, fmt.Errorf("could not determine home directory")
	}

	// Daemon check
	rep.Checks = append(rep.Checks, daemonCheck(ctx, homeDir))

	// Adapters
	if c, err := claude.New(); err != nil {
		rep.Checks = append(rep.Checks, Check{
			Name: "adapter:claude", Status: StatusInfo,
			Detail: "not found on PATH (install Claude Code or Claude CLI)",
		})
	} else {
		rep.Checks = append(rep.Checks, adapterCheck("adapter:claude", c))
	}
	if k, err := kimi.New(); err != nil {
		rep.Checks = append(rep.Checks, Check{
			Name: "adapter:kimi", Status: StatusInfo,
			Detail: "not found on PATH (install kimi CLI to enable)",
		})
	} else {
		rep.Checks = append(rep.Checks, adapterCheck("adapter:kimi", k))
	}

	return rep, nil
}

type adapterIface interface {
	Check() error
	pathReporter
}

func adapterCheck(name string, a adapterIface) Check {
	if err := a.Check(); err != nil {
		return Check{Name: name, Status: StatusFail, Detail: err.Error()}
	}
	return Check{Name: name, Status: StatusOK, Detail: a.Path()}
}

func daemonCheck(ctx context.Context, homeDir string) Check {
	sockDir := filepath.Join(homeDir, ".rallish")
	pointer := filepath.Join(sockDir, "socket")
	pointerBytes, perr := os.ReadFile(pointer) //nolint:gosec // well-known path
	socketPath := strings.TrimSpace(string(pointerBytes))

	if perr == nil && socketPath != "" {
		if uerr := safepath.UnderRoot(socketPath, sockDir); uerr != nil {
			return Check{Name: "daemon", Status: StatusWarn,
				Detail: fmt.Sprintf("socket pointer escapes rallish home: %v", uerr)}
		}
		safePath, cerr := safepath.Clean(socketPath)
		if cerr != nil {
			return Check{Name: "daemon", Status: StatusWarn, Detail: cerr.Error()}
		}
		info, serr := os.Stat(safePath) //nolint:gosec // validated above
		if serr != nil {
			return Check{Name: "daemon", Status: StatusWarn,
				Detail: fmt.Sprintf("pointer present but socket missing: %v", serr)}
		}
		if info.Mode()&fs.ModeSocket == 0 {
			return Check{Name: "daemon", Status: StatusWarn,
				Detail: "pointer references non-socket file"}
		}
		dialCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		s := ipc.Socket{Path: safePath}
		conn, derr := s.Dial(dialCtx)
		if derr != nil {
			return Check{Name: "daemon", Status: StatusFail,
				Detail: fmt.Sprintf("not reachable: %v", derr)}
		}
		_ = conn.Close()
		return Check{Name: "daemon", Status: StatusOK,
			Detail: "reachable via unix socket"}
	}

	if errors.Is(perr, fs.ErrNotExist) || perr == nil {
		portFile := filepath.Join(sockDir, "port")
		if port, rerr := os.ReadFile(portFile); rerr == nil { //nolint:gosec // well-known path
			// A port file can outlive a non-graceful daemon death, so dial before
			// claiming reachability — os.Stat alone falsely reports a dead port OK.
			addr := "127.0.0.1:" + strings.TrimSpace(string(port))
			conn, derr := net.DialTimeout("tcp", addr, 1*time.Second)
			if derr != nil {
				return Check{Name: "daemon", Status: StatusFail,
					Detail: fmt.Sprintf("port file present but not reachable (stale?): %v", derr)}
			}
			_ = conn.Close()
			return Check{Name: "daemon", Status: StatusOK,
				Detail: "reachable via port fallback"}
		}
		return Check{Name: "daemon", Status: StatusInfo,
			Detail: "not running — will auto-spawn on `rally new`"}
	}
	return Check{Name: "daemon", Status: StatusWarn,
		Detail: fmt.Sprintf("could not read socket pointer: %v", perr)}
}

// Run performs health checks for all known adapters and the daemon IPC layer.
// homeDir is the user's home directory; an empty value defaults to os.UserHomeDir.
func Run(ctx context.Context, homeDir string) error {
	logger := slog.Default()

	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	}
	checkDaemon(ctx, logger, homeDir)

	type candidate struct {
		name    string
		checker interface {
			Check() error
			pathReporter
		}
		envList []string
	}

	var candidates []candidate

	if c, err := claude.New(); err != nil {
		logger.Info("adapter not found", "name", "claude", "error", err)
	} else {
		candidates = append(candidates, candidate{
			name:    c.Name(),
			checker: c,
			envList: []string{"PATH", "HOME", "LANG", "TERM", "ANTHROPIC_*"},
		})
	}

	if k, err := kimi.New(); err != nil {
		logger.Info("adapter not found", "name", "kimi", "error", err)
	} else {
		candidates = append(candidates, candidate{
			name:    k.Name(),
			checker: k,
			envList: []string{"PATH", "HOME", "LANG", "TERM", "KIMI_*"},
		})
	}

	for _, c := range candidates {
		if err := c.checker.Check(); err != nil {
			logger.Error("adapter check failed", "name", c.name, "error", err)
		} else {
			logger.Info("adapter check passed", "name", c.name)
		}
		logger.Info("adapter path", "name", c.name, "path", c.checker.Path())
		logger.Info("adapter env allowlist", "name", c.name, "allowlist", c.envList)
	}

	return nil
}

// checkDaemon reports whether the rallish daemon's IPC endpoints are reachable.
// Best-effort: any error is surfaced via slog and does not fail Run.
func checkDaemon(ctx context.Context, logger *slog.Logger, homeDir string) {
	if homeDir == "" {
		return
	}
	sockDir := filepath.Join(homeDir, ".rallish")

	pointer := filepath.Join(sockDir, "socket")
	pointerBytes, perr := os.ReadFile(pointer) //nolint:gosec // well-known path
	socketPath := strings.TrimSpace(string(pointerBytes))

	switch {
	case perr == nil && socketPath != "":
		// Guard: the pointer file is user-owned but its content could be
		// tampered. Refuse paths that escape ~/.rallish/ before touching them.
		if uerr := safepath.UnderRoot(socketPath, sockDir); uerr != nil {
			logger.Warn("daemon socket pointer escapes rallish home; ignoring", "pointer", pointer, "path", socketPath, "error", uerr)
			break
		}
		safePath, cerr := safepath.Clean(socketPath)
		if cerr != nil {
			logger.Warn("daemon socket pointer invalid", "pointer", pointer, "path", socketPath, "error", cerr)
			break
		}
		// gosec G703 taint analysis cannot recognise safepath.UnderRoot +
		// safepath.Clean as sanitisers above; the path is constrained to
		// ~/.rallish/ by that guard.
		info, serr := os.Stat(safePath) //nolint:gosec // path validated by safepath above
		if serr != nil {
			logger.Warn("daemon socket pointer present but socket file missing", "pointer", pointer, "path", safePath, "error", serr)
			break
		}
		mode := info.Mode()
		if mode&fs.ModeSocket == 0 {
			logger.Warn("daemon socket pointer references non-socket file", "path", safePath, "mode", mode.String())
			break
		}
		perm := mode.Perm()
		if perm&0o077 != 0 {
			logger.Warn("daemon socket has loose permissions; expected 0600", "path", safePath, "perm", perm.String())
		}

		dialCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		s := ipc.Socket{Path: safePath}
		conn, derr := s.Dial(dialCtx)
		if derr != nil {
			logger.Warn("daemon unix socket not reachable", "path", safePath, "error", derr)
			break
		}
		_ = conn.Close()
		logger.Info("daemon reachable via unix socket", "path", safePath, "perm", perm.String())
		return
	case errors.Is(perr, fs.ErrNotExist):
		// Fall through to port check.
	default:
		if perr != nil {
			logger.Warn("could not read socket pointer", "path", pointer, "error", perr)
		}
	}

	portFile := filepath.Join(sockDir, "port")
	if _, err := os.Stat(portFile); err == nil {
		logger.Info("daemon port file present; unix socket unavailable", "path", portFile)
	} else if errors.Is(err, fs.ErrNotExist) {
		logger.Info("daemon not running", "checked", []string{pointer, portFile})
	} else {
		logger.Warn("could not stat port file", "path", portFile, "error", err)
	}
}
