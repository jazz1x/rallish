package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/internal/cli"
	"github.com/jazz1x/rallish/internal/skills"
	"github.com/jazz1x/rallish/internal/ui"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

const rootLong = `rallish — agent-driven rally / squash playbook.

A tiny coordinator that lets two coding-CLI agents take turns on the
same problem (rally) or runs one preset headlessly (squash). Skills,
adapters, and presets are bundled and installable via 'rallish add'.

Quick start:
  rallish bootstrap                # one-shot setup wizard
  rallish add                      # interactive component picker
  rallish config list              # see your settings
  rallish doctor                   # check daemon + adapters

For natural-language usage inside a coding-CLI, install the skill
('rallish bootstrap' already does this) and say "랠리보낼 준비해" or
"let's serve" in any project.`

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	shutdown := make(chan struct{})
	isDaemon := false

	root := &cobra.Command{
		Use:           "rallish",
		Short:         "Agent-driven rally / squash playbook",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       buildinfo.String(),
		// Friendlier no-arg behavior: print compact help + hint instead of usage walls.
		Run: func(cmd *cobra.Command, _ []string) {
			t := ui.New()
			t.Heading("rallish")
			t.Detail("agent-driven rally / squash playbook  ·  %s", buildinfo.Version())
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			t.Info("setup:    rallish bootstrap")
			t.Info("install:  rallish add (interactive)")
			t.Info("config:   rallish config list")
			t.Info("verify:   rallish doctor")
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			t.Detail("for a full command tree, run: rallish --help")
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")

	// Group commands for help readability. Cobra renders them under
	// a single "Available Commands" header by default, so we set
	// short groups via Annotations.
	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup"},
		&cobra.Group{ID: "rally", Title: "Rally"},
		&cobra.Group{ID: "manage", Title: "Manage"},
		&cobra.Group{ID: "system", Title: "System"},
	)

	addCmd := cli.AddCmd()
	addCmd.GroupID = "manage"
	configCmd := cli.ConfigCmd()
	configCmd.GroupID = "manage"
	bootstrapCmd := cli.BootstrapCmd()
	bootstrapCmd.GroupID = "setup"
	skillC := skillCmd()
	skillC.GroupID = "setup"
	doctorC := cli.DoctorCmd()
	doctorC.GroupID = "system"
	rallyC := cli.RallyCmd()
	rallyC.GroupID = "rally"
	squashC := squashCmd()
	squashC.GroupID = "rally"
	cycleC := cli.CycleCmd()
	cycleC.GroupID = "rally"
	gateC := cli.GateCmd()
	gateC.GroupID = "system"
	daemonC := daemonCmd(shutdown, &isDaemon)
	daemonC.GroupID = "system"
	versionC := versionCmd()
	versionC.GroupID = "system"

	triggerC := cli.TriggerCmd()
	triggerC.GroupID = "rally"

	root.AddCommand(
		bootstrapCmd,
		addCmd,
		configCmd,
		skillC,
		doctorC,
		rallyC,
		squashC,
		cycleC,
		gateC,
		triggerC,
		daemonC,
		versionC,
	)

	exitCode := 0
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Reset(os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		if isDaemon {
			close(shutdown)
		} else {
			exitCode = 1
		}
	}()

	if err := root.ExecuteContext(context.Background()); err != nil {
		var coded interface{ ExitCode() int }
		switch {
		case errors.As(err, &coded):
			// A command (e.g. `cycle run --once`) that carries its own exit code.
			// The code mapping is the command's SSOT; the root just honours it.
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
			exitCode = coded.ExitCode()
		case errors.Is(err, cli.ErrTimeoutWaitingForBaton):
			exitCode = 2
		case errors.Is(err, contract.ErrInvalidName),
			errors.Is(err, contract.ErrInvalidSessionID),
			errors.Is(err, contract.ErrInvalidPattern):
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
			exitCode = 64 // EX_USAGE
		case errors.Is(err, contract.ErrNotHolder):
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
			exitCode = 1
		default:
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
			exitCode = 1
		}
	}
	os.Exit(exitCode) //nolint:gocritic // defer resets signals; exit code must propagate
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version, commit, and date",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
			return err
		},
	}
}

func daemonCmd(shutdown chan struct{}, isDaemon *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the rallish broker daemon",
		PreRunE: func(_ *cobra.Command, _ []string) error {
			*isDaemon = true
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return cli.RunDaemon(cmd.Context(), home, shutdown)
		},
	}
}

// skillCmd returns the `skill` parent command.
func skillCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "skill",
		Short: "Manage the bundled rallish skill",
	}
	parent.AddCommand(skillInstallCmd())
	return parent
}

// skillInstallCmd returns the `skill install` subcommand.
func skillInstallCmd() *cobra.Command {
	var (
		target string
		name   string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a bundled skill to ~/.claude/skills/<name>",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := target
			if dir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home dir: %w", err)
				}
				dir = filepath.Join(home, ".claude", "skills", name)
			}
			results, err := skills.InstallNamed(name, dir)
			if err != nil {
				return fmt.Errorf("install skill: %w", err)
			}
			t := newOutTheme(cmd.OutOrStdout())
			t.OK("installed skill → %s", dir)
			for _, r := range results {
				t.Detail("%s %s", r.Action, filepath.Base(r.Path))
			}
			// Install companion files for autonomous-cycle.
			if name == "autonomous-cycle" {
				home, _ := os.UserHomeDir()
				companionResults, compErr := skills.InstallCompanionFiles(name, home)
				if compErr != nil {
					t.Warn("companion files: %v", compErr)
				} else {
					for _, r := range companionResults {
						t.Detail("%s %s", r.Action, filepath.Base(r.Path))
					}
				}
			}
			t.Detail("reload your coding-CLI session (or open a new chat) to pick up the skill")
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Install directory (default: ~/.claude/skills/<name>)")
	cmd.Flags().StringVar(&name, "name", "rallish", "Skill name to install (rallish or autonomous-cycle)")
	return cmd
}

// newOutTheme builds a ui.Theme bound to w, color-detected when w is *os.File.
func newOutTheme(w io.Writer) *ui.Theme {
	t := ui.New()
	t.Out = w
	t.ErrOut = w
	if _, ok := w.(*os.File); !ok {
		t.Color = false
	}
	return t
}

func squashCmd() *cobra.Command {
	var opts cli.SquashOptions
	cmd := &cobra.Command{
		Use:   "squash",
		Short: "Run a headless squash session (solo-ralph, pair-review, …)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			opts.HomeDir = home
			return cli.RunSquash(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.PresetName, "preset", "solo-ralph", "Preset to use")
	cmd.Flags().StringVar(&opts.Task, "task", "", "Task description")
	cmd.Flags().StringVar(&opts.SessionID, "session-id", "", "Optional session ID")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository root for the session. Defaults to current working directory.")
	return cmd
}
