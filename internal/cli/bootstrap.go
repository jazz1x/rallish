package cli

// bootstrap.go implements `rallish bootstrap` — the one-shot setup
// walk-through:
//   1. install the skill bundle into ~/.claude/skills/rallish
//   2. (interactive) collect required config values and persist them
//   3. verify the daemon is reachable (or explain auto-spawn behavior)
//
// The wizard is intentionally compact (≤ 1 screen of progress lines)
// so coding-CLI assistants don't truncate their session banner.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jazz1x/rallish/internal/config"
	"github.com/jazz1x/rallish/internal/skills"
	"github.com/jazz1x/rallish/internal/ui"
	"github.com/spf13/cobra"
)

// BootstrapOptions configures the bootstrap wizard.
type BootstrapOptions struct {
	// Yes runs in non-interactive mode (CI-friendly). All prompts
	// pick their default; the config file gets the built-in defaults.
	Yes bool

	// SkipSkill skips the skill-bundle install (useful when the user
	// has already placed it under a different path).
	SkipSkill bool

	// SkipConfig skips the config-wizard step (still creates a
	// defaults-only config file if none exists).
	SkipConfig bool

	stdout io.Writer
	stdin  io.Reader
}

func (o *BootstrapOptions) theme() *ui.Theme {
	t := ui.New()
	if o.stdout != nil {
		t.Out = o.stdout
		t.ErrOut = o.stdout
		t.Color = false
	}
	if o.stdin != nil {
		t.In = o.stdin
	}
	return t
}

// BootstrapCmd returns the `bootstrap` cobra command.
func BootstrapCmd() *cobra.Command {
	var opts BootstrapOptions
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Set up rallish in one step (skill + config + daemon check)",
		Long: `Run a compact, interactive wizard that:
  1. installs the rallish skill into ~/.claude/skills/rallish
  2. collects required config values (default preset, adapter, wait mode)
  3. verifies the daemon

The wizard fits in one screen of progress lines so coding-CLI assistants
don't truncate their session banner. Use --yes to skip all prompts.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunBootstrap(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip prompts; accept defaults")
	cmd.Flags().BoolVar(&opts.SkipSkill, "skip-skill", false, "Don't reinstall the skill bundle")
	cmd.Flags().BoolVar(&opts.SkipConfig, "skip-config", false, "Don't prompt for config values")
	return cmd
}

// RunBootstrap is the executable entry point so tests can call it
// directly with an in-memory BootstrapOptions. The context is checked
// between steps so a Ctrl-C / SIGTERM cancellation exits cleanly
// without writing a partial config.
func RunBootstrap(ctx context.Context, opts BootstrapOptions) error {
	t := opts.theme()
	total := 4

	// Banner — three short lines. Keeps the discography visible.
	t.Heading("rallish setup")
	t.Detail("4 quick steps · enter to accept defaults · ctrl-c to abort")

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	// Step 1 — install skill.
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.SkipSkill {
		t.StepOK(1, total, "skill install — skipped")
	} else {
		skillDir := filepath.Join(home, ".claude", "skills", "rallish")
		t.Step(1, total, "installing skill → %s", skillDir)
		results, err := skills.Install(skillDir)
		if err != nil {
			t.StepErr(1, total, "skill install failed: %v", err)
			return err
		}
		for _, r := range results {
			t.Detail("%s %s", r.Action, filepath.Base(r.Path))
		}

		// Install the MCP client skill as well.
		mcpSkillDir := filepath.Join(home, ".claude", "skills", "rallish-mcp")
		t.Detail("installing mcp skill → %s", mcpSkillDir)
		mcpResults, mcpErr := skills.InstallNamed("rallish-mcp", mcpSkillDir)
		if mcpErr != nil {
			t.StepErr(1, total, "mcp skill install failed: %v", mcpErr)
			return mcpErr
		}
		for _, r := range mcpResults {
			t.Detail("%s %s", r.Action, filepath.Base(r.Path))
		}
		// Install autonomous-cycle companion files (scripts + runbooks).
		companionResults, compErr := skills.InstallCompanionFiles("autonomous-cycle", home)
		if compErr != nil {
			t.Warn("companion files: %v", compErr)
		} else {
			for _, r := range companionResults {
				t.Detail("%s %s", r.Action, filepath.Base(r.Path))
			}
		}
		t.StepOK(1, total, "skills installed (%d files)", len(results)+len(mcpResults)+len(companionResults))
	}

	// Step 2 — config wizard.
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.SkipConfig {
		t.StepOK(2, total, "config wizard — skipped")
		ensureConfigFile(t)
	} else {
		if err := runConfigWizard(t, opts); err != nil {
			t.StepErr(2, total, "config wizard: %v", err)
			return err
		}
		t.StepOK(2, total, "config saved")
	}

	// Step 3 — show config summary.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := showConfigSummary(t, 3, total); err != nil {
		return err
	}

	// Step 4 — daemon status.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := showDaemonStatus(t, 4, total, home); err != nil {
		return err
	}

	// Footer — single trigger phrase + a how-to-customize hint.
	t.Footer("setup complete — say \"랠리보낼 준비해\" (or \"let's serve\") to start")
	t.Detail("change settings later: rallish config list / set / edit")
	return nil
}

// runConfigWizard collects the most-asked-about config values. Tests
// pass --yes to skip the prompts and write defaults verbatim.
func runConfigWizard(t *ui.Theme, opts BootstrapOptions) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}

	if opts.Yes {
		return config.Save(cfg)
	}

	// Default preset.
	preset, err := t.Ask("Default preset (used by `rallish squash`)?", cfg.DefaultPreset)
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, "default_preset", preset); err != nil {
		return err
	}

	// Default adapter / coding-CLI vendor — pick from known set.
	choices := []string{"claude", "kimi", "codex"}
	def := indexOf(choices, cfg.CodingCLI)
	if def < 0 {
		def = 0
	}
	cli, _, err := t.SelectOne("Which coding-CLI vendor do you use day-to-day?", choices, def)
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, "coding_cli", cli); err != nil {
		return err
	}
	if err := config.Set(&cfg, "default_adapter", cli); err != nil {
		// Adapter and coding_cli are decoupled — but for first-time users
		// matching them is a safe default. Ignore mismatch errors silently.
		_ = err
	}

	// Wait mode.
	wm, _, err := t.SelectOne("Rally wait mode?",
		[]string{"yield (poll on each user message — default)", "block (blocking baton join)"},
		0)
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, "wait_mode", firstWord(wm)); err != nil {
		return err
	}

	return config.Save(cfg)
}

// ensureConfigFile creates a defaults-only file if none exists yet, so
// `rallish config edit` later doesn't have to bootstrap.
func ensureConfigFile(t *ui.Theme) {
	path, err := config.Path()
	if err != nil {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	cfg, _, err := config.Load()
	if err != nil {
		return
	}
	if err := config.Save(cfg); err != nil {
		t.Warn("could not write %s: %v", path, err)
		return
	}
	t.Detail("created %s with built-in defaults", path)
}

// showConfigSummary prints a Kv-style block for the three values the
// wizard asks about so the user can scan them at a glance.
func showConfigSummary(t *ui.Theme, idx, total int) error {
	cfg, res, err := config.Load()
	if err != nil {
		return err
	}
	t.Step(idx, total, "config summary")
	path, _ := config.Path()
	source := "defaults"
	if res.FileExists {
		source = path
	}
	t.KvSource("default_preset", cfg.DefaultPreset, source)
	t.KvSource("coding_cli", cfg.CodingCLI, source)
	t.KvSource("wait_mode", cfg.WaitMode, source)
	t.StepOK(idx, total, "config OK")
	return nil
}

// showDaemonStatus probes ~/.rallish/socket and ~/.rallish/port.
// Missing daemon is reported as "will auto-spawn" — not an error.
func showDaemonStatus(t *ui.Theme, idx, total int, home string) error {
	t.Step(idx, total, "daemon check")
	sockDir := filepath.Join(home, ".rallish")
	pointerBytes, perr := os.ReadFile(filepath.Join(sockDir, "socket")) //nolint:gosec // well-known path
	if perr == nil && len(pointerBytes) > 0 {
		t.StepOK(idx, total, "daemon reachable")
		return nil
	}
	if _, ferr := os.Stat(filepath.Join(sockDir, "port")); ferr == nil {
		t.StepOK(idx, total, "daemon reachable (port-fallback)")
		return nil
	}
	t.StepOK(idx, total, "daemon not running — will auto-spawn on `rally new`")
	return nil
}

// indexOf returns the position of v in s, or -1 when missing.
func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// firstWord returns the first whitespace-separated token of s. Used to
// pick the short label out of a "yield (long description …)" choice.
func firstWord(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
