package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/jazz1x/hocketty/internal/buildinfo"
	"github.com/spf13/cobra"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	root := &cobra.Command{
		Use:   "hocketty",
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
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "doctor: not fully implemented in phase 0")
				return err
			},
		},
	)

	if err := fang.Execute(context.Background(), root, fang.WithoutVersion()); err != nil {
		os.Exit(1)
	}
}
