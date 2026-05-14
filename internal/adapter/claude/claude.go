// Package claude implements the hocketty adapter for the Claude CLI runtime.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jazz1x/hocketty/internal/adapter"
	"github.com/jazz1x/hocketty/pkg/contract"
)

// Adapter executes turns via the Claude CLI.
type Adapter struct {
	binary string
}

// New resolves the claude binary and returns an Adapter.
func New() (*Adapter, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving claude path: %w", err)
	}
	return &Adapter{binary: abs}, nil
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "claude" }

// Path returns the resolved absolute path to the binary.
func (a *Adapter) Path() string { return a.binary }

// Check verifies the binary exists and is executable.
func (a *Adapter) Check() error {
	if a.binary == "" {
		return errors.New("claude binary path not set")
	}
	info, err := os.Stat(a.binary)
	if err != nil {
		return fmt.Errorf("claude binary not accessible: %w", err)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("claude binary is not executable: %s", a.binary)
	}
	return nil
}

// buildCmd constructs the exec.Cmd for a turn without running it.
func (a *Adapter) buildCmd(ctx context.Context, req contract.TurnRequest) (*exec.Cmd, error) {
	prompt, err := adapter.BuildPrompt(req)
	if err != nil {
		return nil, fmt.Errorf("building prompt: %w", err)
	}

	//nolint:gosec // G204 — args are built from controlled inputs
	cmd := exec.CommandContext(ctx, a.binary, "-p", prompt, "--max-turns=1")
	cmd.Env = adapter.BuildEnv("PATH", "HOME", "LANG", "TERM", "USER", "LOGNAME", "SHELL", "TMPDIR", "XDG_CONFIG_HOME", "ANTHROPIC_")
	if req.Task.RepoRoot != "" {
		cmd.Dir = req.Task.RepoRoot
	}
	return cmd, nil
}

// Run executes a single turn via the Claude CLI.
func (a *Adapter) Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error) {
	cmd, err := a.buildCmd(ctx, req)
	if err != nil {
		return contract.TurnResponse{}, err
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return contract.TurnResponse{}, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return contract.TurnResponse{}, fmt.Errorf("running claude: %w", err)
	}

	var resp contract.TurnResponse
	if err := adapter.ParseLastJSONBlock(out, &resp); err != nil {
		return contract.TurnResponse{}, fmt.Errorf("parsing response: %w", err)
	}
	return resp, nil
}
