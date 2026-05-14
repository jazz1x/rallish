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

	"github.com/jazz1x/hocketty/internal/preset"
	"github.com/jazz1x/hocketty/internal/runner"
	"github.com/jazz1x/hocketty/pkg/contract"
	"golang.org/x/sync/errgroup"
)

// StartOptions holds flags for the start command.
type StartOptions struct {
	PresetName     string
	Task           string
	SessionID      string
	AllowShellExit bool
	HomeDir        string
}

// RunStart loads a preset, ensures the daemon is running, creates a session, and starts runners.
func RunStart(ctx context.Context, opts StartOptions) error {
	presets, err := preset.LoadEmbedded()
	if err != nil {
		return fmt.Errorf("load embedded presets: %w", err)
	}

	p, ok := presets[opts.PresetName]
	if !ok {
		path := filepath.Join(opts.HomeDir, ".hocketty", "presets", opts.PresetName+".yaml")
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

	sockDir := filepath.Join(opts.HomeDir, ".hocketty")
	portFile := filepath.Join(sockDir, "port")
	port, err := readPortFile(portFile)
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
		}
		if err != nil {
			return fmt.Errorf("daemon did not write port file: %w", err)
		}
	}

	brokerURL := fmt.Sprintf("http://127.0.0.1:%s", strings.TrimSpace(port))

	createBody := struct {
		Preset contract.Preset `json:"preset"`
		Task   contract.Task   `json:"task"`
	}{
		Preset: p,
		Task: contract.Task{
			Title:    opts.Task,
			Body:     opts.Task,
			RepoRoot: opts.HomeDir,
		},
	}
	bodyJSON, err := json.Marshal(createBody)
	if err != nil {
		return fmt.Errorf("marshal create body: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
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

	sessionID := sess.ID
	if opts.SessionID != "" {
		// Phase 3 MVP ignores custom session IDs because the broker generates them.
		_ = opts.SessionID
	}

	fmt.Println(sessionID)

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
			err := runner.NewLoop(a, r.ID, brokerURL, sessionID).Run(ctx)
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

	logPath := filepath.Join(opts.HomeDir, ".hocketty", "sessions", sessionID, "log.jsonl")
	fmt.Printf("Session %s completed successfully. Log: %s\n", sessionID, logPath)
	return nil
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
