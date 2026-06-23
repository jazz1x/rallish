package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/ipc"
	"github.com/jazz1x/rallish/internal/preset"
	"github.com/jazz1x/rallish/internal/runner"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
	"golang.org/x/sync/errgroup"
)

// SquashOptions holds flags for the squash command.
type SquashOptions struct {
	PresetName string
	Task       string
	SessionID  string
	HomeDir    string
	Repo       string
}

// RunSquash loads a preset, ensures the daemon is running, creates a session, and starts runners.
func RunSquash(ctx context.Context, opts SquashOptions) error {
	repoRoot := opts.Repo
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(repoRoot)
	if err != nil {
		return fmt.Errorf("repo path does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repo path is not a directory: %s", repoRoot)
	}

	sockDir := filepath.Join(opts.HomeDir, ".rallish")

	var brokerURL string
	var client *http.Client

	socketFile := filepath.Join(sockDir, "socket")
	socketPath, readErr := os.ReadFile(socketFile) //nolint:gosec // well-known path
	trimmed := strings.TrimSpace(string(socketPath))
	// A live, in-root socket pointer wins. But the pointer (and socket file)
	// survive a non-graceful daemon death (kill -9, OOM, reboot): without a
	// liveness probe we would reuse the dead socket and fail at create session
	// with a cryptic "connection refused" instead of auto-spawning a fresh
	// daemon. Probe the socket; on a dead one, remove the stale pointer and fall
	// through to the port/auto-spawn path, mirroring resolveBrokerClient.
	socketLive := readErr == nil && trimmed != "" && socketUnderRoot(trimmed, sockDir) &&
		dialExistingDaemon(trimmed, 300*time.Millisecond) == nil
	if socketLive {
		brokerURL = "http://rallish.local"
		client = ipc.HTTPClientOverSocket(trimmed)
		client.Timeout = 30 * time.Second
	} else {
		if readErr == nil && trimmed != "" {
			if socketUnderRoot(trimmed, sockDir) {
				_ = os.Remove(socketFile) // stale in-root pointer
			} else {
				slog.Warn("ignoring socket pointer outside rallish home", "path", trimmed)
			}
		}
		portFile := filepath.Join(sockDir, "port")
		port, err := readPortFile(portFile)
		// A stale port file outlives a non-graceful daemon death just like the
		// socket pointer, so a successful read is not enough — probe it. A dead
		// (or missing) port both mean "no live broker": remove the stale file and
		// auto-spawn, rather than reusing a dead port and failing at create
		// session with a cryptic "connection refused".
		if err == nil && dialPort(strings.TrimSpace(port)) != nil {
			_ = os.Remove(portFile)
			err = errors.New("stale port file removed")
		}
		if err != nil {
			slog.Info("broker not running, starting daemon")
			if err := startDaemon(opts.HomeDir); err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}
			for i := 0; i < 20; i++ {
				port, err = readPortFile(portFile)
				if err == nil {
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			if err != nil {
				return fmt.Errorf("daemon did not write port file: %w", err)
			}
		}

		brokerURL = fmt.Sprintf("http://127.0.0.1:%s", strings.TrimSpace(port))
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var sessionID string
	var p contract.Preset

	if opts.SessionID != "" {
		// Resume existing session.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL+"/sessions/"+opts.SessionID, nil)
		if err != nil {
			return fmt.Errorf("build resume request: %w", err)
		}
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("resume session: %w", err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode == http.StatusNotFound {
			return fmt.Errorf("session %q not found on broker", opts.SessionID)
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("resume session: status %d", res.StatusCode)
		}
		var sess session.Session
		if err := json.NewDecoder(res.Body).Decode(&sess); err != nil {
			return fmt.Errorf("decode session: %w", err)
		}
		sessionID = sess.ID

		presets, err := preset.LoadEmbedded()
		if err != nil {
			return fmt.Errorf("load embedded presets: %w", err)
		}
		var ok bool
		p, ok = presets[sess.PresetName]
		if !ok {
			path := filepath.Join(opts.HomeDir, ".rallish", "presets", sess.PresetName+".yaml")
			f, err := os.Open(path) //nolint:gosec // path under user home
			if err != nil {
				return fmt.Errorf("preset %q not found: %w", sess.PresetName, err)
			}
			defer func() { _ = f.Close() }()
			p, err = preset.Load(f)
			if err != nil {
				return fmt.Errorf("load preset %q: %w", sess.PresetName, err)
			}
		}
	} else {
		// Create new session.
		if opts.Task == "" {
			return fmt.Errorf("--task is required")
		}

		presets, err := preset.LoadEmbedded()
		if err != nil {
			return fmt.Errorf("load embedded presets: %w", err)
		}
		var ok bool
		p, ok = presets[opts.PresetName]
		if !ok {
			path := filepath.Join(opts.HomeDir, ".rallish", "presets", opts.PresetName+".yaml")
			f, err := os.Open(path) //nolint:gosec // path under user home
			if err != nil {
				return fmt.Errorf("preset %q not found: %w", opts.PresetName, err)
			}
			defer func() { _ = f.Close() }()
			p, err = preset.Load(f)
			if err != nil {
				return fmt.Errorf("load preset file: %w", err)
			}
		}

		createBody := struct {
			Preset contract.Preset `json:"preset"`
			Task   contract.Task   `json:"task"`
		}{
			Preset: p,
			Task: contract.Task{
				Title:    opts.Task,
				Body:     opts.Task,
				RepoRoot: repoRoot,
			},
		}
		bodyJSON, err := json.Marshal(createBody)
		if err != nil {
			return fmt.Errorf("marshal create body: %w", err)
		}

		//nolint:gosec // local broker URL
		resp, err := client.Post(brokerURL+"/sessions", "application/json", bytes.NewReader(bodyJSON))
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusCreated {
			data, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("create session failed: %d %s", resp.StatusCode, string(data))
		}

		var sess struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
			return fmt.Errorf("decode session: %w", err)
		}
		sessionID = sess.ID
	}

	_, _ = fmt.Fprintln(os.Stdout, sessionID)

	reg, err := BuildRegistry()
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, role := range p.Roles {
		a, ok := reg.Get(role.Runtime)
		if !ok {
			return fmt.Errorf("unknown runtime %q for role %q", role.Runtime, role.ID)
		}
		r := role
		g.Go(func() error {
			loop := runner.NewLoopWithClient(a, r.ID, brokerURL, sessionID, client)
			err := loop.Run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("runner loop exited", "role", r.ID, "error", err)
				return err
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	logPath := filepath.Join(opts.HomeDir, ".rallish", "sessions", sessionID, "log.jsonl")
	_, _ = fmt.Fprintf(os.Stdout, "Session %s in %s completed successfully. Log: %s\n", sessionID, repoRoot, logPath)
	return nil
}

// socketUnderRoot returns true when path resolves to a location inside sockDir,
// guarding against symlink or pointer-file tampering (PRD §6).
func socketUnderRoot(path, sockDir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(sockDir)
	if err != nil {
		return false
	}
	cleanPath := filepath.Clean(absPath)
	cleanDir := filepath.Clean(absDir)
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readPortFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // well-known path
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func startDaemon(_ string) error {
	cmd := exec.Command(os.Args[0], "daemon") //nolint:gosec // self-execution
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}
	return nil
}
