// Package kimi implements the rallish adapter for the Kimi CLI runtime.
package kimi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/pkg/contract"
)

// Adapter executes turns via the Kimi CLI.
type Adapter struct {
	binary string
}

// envAllowlist is the single source of truth for the environment keys/prefixes
// passed through to the kimi subprocess (shared by buildCmd and Probe).
var envAllowlist = []string{
	"PATH", "HOME", "LANG", "TERM", "USER", "LOGNAME", "SHELL", "TMPDIR", "XDG_CONFIG_HOME", "KIMI_",
}

// New resolves the kimi binary and returns an Adapter.
func New() (*Adapter, error) {
	path, err := exec.LookPath("kimi")
	if err != nil {
		return nil, fmt.Errorf("kimi binary not found: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving kimi path: %w", err)
	}
	return &Adapter{binary: abs}, nil
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "kimi" }

// Path returns the resolved absolute path to the binary.
func (a *Adapter) Path() string { return a.binary }

// Check verifies the binary exists and is executable.
func (a *Adapter) Check() error {
	if a.binary == "" {
		return errors.New("kimi binary path not set")
	}
	info, err := os.Stat(a.binary)
	if err != nil {
		return fmt.Errorf("kimi binary not accessible: %w", err)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("kimi binary is not executable: %s", a.binary)
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
	cmd := exec.CommandContext(ctx, a.binary, "-p", prompt)
	cmd.Env = adapter.BuildEnv(envAllowlist...)
	if req.Task.RepoRoot != "" {
		cmd.Dir = req.Task.RepoRoot
	}
	return cmd, nil
}

// Run executes a single turn via the Kimi CLI.
func (a *Adapter) Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error) {
	cmd, err := a.buildCmd(ctx, req)
	if err != nil {
		return contract.TurnResponse{}, err
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if hint, _, ok := adapter.DiagnoseOutput("kimi", string(out)+"\n"+string(exitErr.Stderr)); ok {
				return contract.TurnResponse{}, fmt.Errorf("%s\nstderr: %s", hint, string(exitErr.Stderr))
			}
			return contract.TurnResponse{}, fmt.Errorf("kimi exited with error: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return contract.TurnResponse{}, fmt.Errorf("running kimi: %w", err)
	}

	var resp contract.TurnResponse
	if err := adapter.ParseLastJSONBlock(out, &resp); err != nil {
		if hint, _, ok := adapter.DiagnoseOutput("kimi", string(out)); ok {
			return contract.TurnResponse{}, errors.New(hint)
		}
		return contract.TurnResponse{}, fmt.Errorf("parsing response: %w\noutput: %s", err, adapter.Snippet(out, 2000))
	}
	return resp, nil
}

// Probe runs a minimal real turn to verify the kimi CLI is reachable and
// authenticated. Used by `doctor --probe` (opt-in, spends one cheap turn).
func (a *Adapter) Probe(ctx context.Context) error {
	//nolint:gosec // G204 — fixed args, no user input
	cmd := exec.CommandContext(ctx, a.binary, "-p", "respond with the single word: ok")
	cmd.Env = adapter.BuildEnv(envAllowlist...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		var stderr string
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		if hint, _, ok := adapter.DiagnoseOutput("kimi", string(out)+"\n"+stderr); ok {
			return errors.New(hint)
		}
		return fmt.Errorf("kimi probe failed: %w", err)
	}
	if hint, _, ok := adapter.DiagnoseOutput("kimi", string(out)); ok {
		return errors.New(hint)
	}
	return nil
}
