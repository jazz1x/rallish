package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/internal/cli"
	"github.com/jazz1x/rallish/internal/doctor"
	"github.com/spf13/cobra"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	root := &cobra.Command{
		Use:   "rallish",
		Short: "A local broker for multi-agent turn-taking",
	}

	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print version information",
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
				return err
			},
		},
		&cobra.Command{
			Use:   "doctor",
			Short: "Check adapters, paths, and permissions",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return doctor.Run(cmd.Context())
			},
		},
		daemonCmd(),
		startCmd(),
	)

	if err := fang.Execute(context.Background(), root, fang.WithoutVersion()); err != nil {
		os.Exit(1)
	}
}

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the rallish broker daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return cli.RunDaemon(cmd.Context(), home)
		},
	}
}

func startCmd() *cobra.Command {
	var opts cli.StartOptions
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new rallish session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			opts.HomeDir = home
			return cli.RunStart(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.PresetName, "preset", "solo-ralph", "Preset to use")
	cmd.Flags().StringVar(&opts.Task, "task", "", "Task description")
	cmd.Flags().StringVar(&opts.SessionID, "session-id", "", "Optional session ID")
	cmd.Flags().BoolVar(&opts.AllowShellExit, "allow-shell-exit", false, "Allow shell exit predicates")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository root for the session. Defaults to current working directory.")
	return cmd
}
