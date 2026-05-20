package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/config"
	"github.com/stretchr/testify/require"
)

// withBootstrapHome isolates HOME and RALLISH_HOME so the bootstrap
// step writes into a temp tree.
func withBootstrapHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("RALLISH_HOME", filepath.Join(tmp, ".rallish"))
	return tmp
}

func TestBootstrap_Yes_NonInteractive(t *testing.T) {
	tmp := withBootstrapHome(t)
	var out bytes.Buffer
	opts := BootstrapOptions{Yes: true, stdout: &out}
	require.NoError(t, RunBootstrap(context.Background(), opts))

	s := out.String()
	require.Contains(t, s, "rallish setup")
	require.Contains(t, s, "[1/4]")
	require.Contains(t, s, "[4/4]")
	require.Contains(t, s, "setup complete")

	// Config file should have been written with defaults.
	cfgPath := filepath.Join(tmp, ".rallish", "config.yaml")
	cfg, res, err := config.Load()
	require.NoError(t, err)
	require.True(t, res.FileExists, "config file should exist at %s", cfgPath)
	require.Equal(t, config.Defaults().DefaultPreset, cfg.DefaultPreset)

	// Skill files should have landed.
	skillPath := filepath.Join(tmp, ".claude", "skills", "rallish", "SKILL.md")
	require.FileExists(t, skillPath)
}

func TestBootstrap_SkipSkill_SkipConfig(t *testing.T) {
	withBootstrapHome(t)
	var out bytes.Buffer
	opts := BootstrapOptions{Yes: true, SkipSkill: true, SkipConfig: true, stdout: &out}
	require.NoError(t, RunBootstrap(context.Background(), opts))
	s := out.String()
	require.Contains(t, s, "skill install — skipped")
	require.Contains(t, s, "config wizard — skipped")
}

func TestBootstrap_BannerFitsScreen(t *testing.T) {
	// Confirm the wizard output is short enough not to push a 24-row
	// terminal off-screen. We render with --yes (densest output).
	withBootstrapHome(t)
	var out bytes.Buffer
	opts := BootstrapOptions{Yes: true, stdout: &out}
	require.NoError(t, RunBootstrap(context.Background(), opts))
	lines := strings.Count(out.String(), "\n")
	require.Less(t, lines, 50, "bootstrap should fit on one screen; got %d lines", lines)
}

func TestBootstrap_InteractiveWizard_SetsValues(t *testing.T) {
	withBootstrapHome(t)
	// Provide stdin answers:
	//   1. preset → "pair-review"
	//   2. coding_cli select → "2" (kimi)
	//   3. wait_mode select → "2" (block)
	stdin := strings.NewReader("pair-review\n2\n2\n")
	var out bytes.Buffer
	opts := BootstrapOptions{stdin: stdin, stdout: &out}
	require.NoError(t, RunBootstrap(context.Background(), opts))

	cfg, res, err := config.Load()
	require.NoError(t, err)
	require.True(t, res.FileExists)
	require.Equal(t, "pair-review", cfg.DefaultPreset)
	require.Equal(t, "kimi", cfg.CodingCLI)
	require.Equal(t, "block", cfg.WaitMode)
}
