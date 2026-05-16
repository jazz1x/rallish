package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/internal/cli"
	"github.com/jazz1x/rallish/internal/doctor"
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
		exitCode = 1
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
