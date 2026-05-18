package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/internal/cli"
	"github.com/jazz1x/rallish/internal/doctor"
	"github.com/jazz1x/rallish/internal/skills"
	"github.com/spf13/cobra"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	shutdown := make(chan struct{})
	isDaemon := false

	root := &cobra.Command{Use: "rallish", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(
		&cobra.Command{Use: "version", RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
			return err
		}},
		&cobra.Command{Use: "doctor", Short: "Diagnose adapters and daemon reachability", RunE: func(cmd *cobra.Command, _ []string) error {
			home, _ := os.UserHomeDir()
			return doctor.Run(cmd.Context(), home)
		}},
		daemonCmd(shutdown, &isDaemon),
		squashCmd(),
		cli.RallyCmd(),
		cli.AddCmd(),
		skillCmd(),
		bootstrapCmd(),
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
		if errors.Is(err, cli.ErrTimeoutWaitingForBaton) {
			exitCode = 2
		} else {
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
			exitCode = 1
		}
	}
	os.Exit(exitCode) //nolint:gocritic // defer resets signals; exit code must propagate
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

// defaultSkillTarget returns the default install directory for the
// rallish-operator skill: ~/.claude/skills/rallish-operator.
func defaultSkillTarget() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "skills", "rallish-operator"), nil
}

// skillCmd returns the `skill` parent command.
func skillCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "skill",
		Short: "Manage bundled skills",
	}
	parent.AddCommand(skillInstallCmd())
	return parent
}

// skillInstallCmd returns the `skill install` subcommand.
func skillInstallCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the rallish-operator skill to ~/.claude/skills/rallish-operator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := target
			if dir == "" {
				var err error
				dir, err = defaultSkillTarget()
				if err != nil {
					return err
				}
			}
			results, err := skills.Install(dir)
			if err != nil {
				return fmt.Errorf("install skill: %w", err)
			}
			out := cmd.OutOrStdout()
			for _, r := range results {
				_, _ = fmt.Fprintf(out, "%s %s\n", r.Action, r.Path)
			}
			_, _ = fmt.Fprintln(out, "Reload your coding-CLI session (or open a new chat) to pick up the skill.")
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Install directory (default: ~/.claude/skills/rallish-operator)")
	return cmd
}

// bootstrapCmd returns the `bootstrap` command.
func bootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Install the rallish-operator skill and verify the daemon in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}

			skillDir := filepath.Join(home, ".claude", "skills", "rallish-operator")

			// 1. Install skill.
			results, err := skills.Install(skillDir)
			if err != nil {
				return fmt.Errorf("install skill: %w", err)
			}
			for _, r := range results {
				_, _ = fmt.Fprintf(out, "%s %s\n", r.Action, r.Path)
			}

			// 2. Run doctor.
			if err := doctor.Run(cmd.Context(), home); err != nil {
				// doctor surfaces issues via slog; treat non-nil as informational.
				_, _ = fmt.Fprintf(out, "doctor: %v\n", err)
			}

			// 3. Determine daemon status for the summary block.
			daemonStatus := "not running — will auto-spawn on next `rallish rally new`"
			sockDir := filepath.Join(home, ".rallish")
			pointerBytes, perr := os.ReadFile(filepath.Join(sockDir, "socket")) //nolint:gosec // well-known path
			if perr == nil && len(pointerBytes) > 0 {
				daemonStatus = "reachable"
			} else if _, ferr := os.Stat(filepath.Join(sockDir, "port")); ferr == nil {
				daemonStatus = "reachable"
			}

			_, _ = fmt.Fprintf(out, "\nBootstrap complete.\n")
			_, _ = fmt.Fprintf(out, "- Skill installed at: %s\n", skillDir)
			_, _ = fmt.Fprintf(out, "- Daemon: %s\n", daemonStatus)
			_, _ = fmt.Fprintln(out, "- Next: open any project in Claude Code and say \"랠리보낼 준비해\" to start.")
			return nil
		},
	}
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
	cmd.Flags().BoolVar(&opts.AllowShellExit, "allow-shell-exit", false, "Allow shell exit predicates")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository root for the session. Defaults to current working directory.")
	return cmd
}
