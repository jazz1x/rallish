package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// componentInfo describes an installable built-in component.
type componentInfo struct {
	Name        string
	Description string
	Version     string
}

// builtinCatalog is the registry of built-in adapters and presets.
var builtinCatalog = map[string]map[string]componentInfo{
	"adapter": {
		"claude": {Name: "claude", Description: "Claude Code adapter", Version: "0.0.1"},
		"kimi":   {Name: "kimi", Description: "Kimi adapter", Version: "0.0.1"},
		"fake":   {Name: "fake", Description: "Fake adapter for testing", Version: "0.0.1"},
	},
	"preset": {
		"solo-ralph":  {Name: "solo-ralph", Description: "Single-agent execution with budget guards", Version: "0.0.1"},
		"pair-review": {Name: "pair-review", Description: "Planner / executor / reviewer turn-taking", Version: "0.0.1"},
	},
}

// AddCmd returns the `add` cobra command tree.
func AddCmd() *cobra.Command {
	var list bool
	var global bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Install adapters, presets, or skills",
		Long:  "Install components into your project (./.rallish/) or globally (~/.rallish/).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if list {
				return printCatalog(cmd)
			}
			return fmt.Errorf("usage: rallish add [adapter|preset] NAME\n       rallish add --list")
		},
	}

	cmd.Flags().BoolVarP(&list, "list", "l", false, "List installable components")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Install into ~/.rallish/ instead of ./.rallish/")

	cmd.AddCommand(
		addAdapterCmd(&global),
		addPresetCmd(&global),
	)

	return cmd
}

func addAdapterCmd(global *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "adapter <name>",
		Short: "Install an adapter",
		Example: `  rallish add adapter claude
  rallish add adapter claude --global`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("adapter name required; run 'rallish add --list' to see options")
			}
			return installAdapter(cmd, args[0], *global)
		},
	}
}

func addPresetCmd(global *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "preset <name>",
		Short: "Install a preset",
		Example: `  rallish add preset pair-review
  rallish add preset solo-ralph --global`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("preset name required; run 'rallish add --list' to see options")
			}
			return installPreset(cmd, args[0], *global)
		},
	}
}

func printCatalog(cmd *cobra.Command) error {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Available components:")
	_, _ = fmt.Fprintln(cmd.OutOrStdout())

	for kind, items := range builtinCatalog {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s:\n", kind)
		for name, info := range items {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    %-15s %s\n", name, info.Description)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Usage:")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  rallish add adapter <name>")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  rallish add preset <name>")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  rallish add adapter <name> --global")
	return nil
}

func installAdapter(cmd *cobra.Command, name string, global bool) error {
	adapters, ok := builtinCatalog["adapter"]
	if !ok {
		return fmt.Errorf("unknown adapter %q; run 'rallish add --list'", name)
	}
	info, ok := adapters[name]
	if !ok {
		return fmt.Errorf("unknown adapter %q; run 'rallish add --list'", name)
	}

	dir, err := targetDir(global, "adapters")
	if err != nil {
		return err
	}

	// Adapters are compiled in; installation writes a marker file.
	marker := filepath.Join(dir, name+".installed")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create adapters dir: %w", err)
	}
	if err := os.WriteFile(marker, []byte(fmt.Sprintf("name: %s\nversion: %s\n", info.Name, info.Version)), 0o600); err != nil {
		return fmt.Errorf("write adapter marker: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Installed adapter %q (%s)\n", info.Name, info.Version)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Location: %s\n", marker)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  To use: set 'adapter: "+name+"' in your preset")
	return nil
}

func installPreset(cmd *cobra.Command, name string, global bool) error {
	presets, ok := builtinCatalog["preset"]
	if !ok {
		return fmt.Errorf("unknown preset %q; run 'rallish add --list'", name)
	}
	info, ok := presets[name]
	if !ok {
		return fmt.Errorf("unknown preset %q; run 'rallish add --list'", name)
	}

	// Find the embedded preset file.
	src := filepath.Join("internal", "preset", "presets", name+".yaml")
	data, err := os.ReadFile(src) //nolint:gosec // built-in path
	if err != nil {
		return fmt.Errorf("read built-in preset %q: %w", name, err)
	}

	dir, err := targetDir(global, "presets")
	if err != nil {
		return err
	}

	dst := filepath.Join(dir, name+".yaml")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create presets dir: %w", err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("write preset: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Installed preset %q (%s)\n", info.Name, info.Version)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Location: %s\n", dst)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  To use: rallish start --preset "+name)
	return nil
}

func targetDir(global bool, subdir string) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, ".rallish", subdir), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	return filepath.Join(cwd, ".rallish", subdir), nil
}
