package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/jazz1x/rallish/internal/config"
	"github.com/jazz1x/rallish/internal/ui"
	"github.com/spf13/cobra"
)

// ConfigOptions plumbs streams for unit tests.
type ConfigOptions struct {
	stdout io.Writer
	stdin  io.Reader

	// runEditor is overridable for tests so we don't fork a real editor.
	runEditor func(editor, path string) error
}

func (o *ConfigOptions) out() io.Writer {
	if o.stdout != nil {
		return o.stdout
	}
	return os.Stdout
}

func (o *ConfigOptions) theme() *ui.Theme {
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

// ConfigCmd returns the `config` cobra command group.
func ConfigCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit rallish settings",
		Long: `Inspect and edit rallish settings stored at ~/.rallish/config.yaml.

Examples:
  rallish config list                  # show all keys with source
  rallish config get default_preset    # print one value
  rallish config set wait_mode block   # change a value
  rallish config path                  # print the config file path
  rallish config edit                  # open the file in $EDITOR

The config file is created on the first 'set' or 'bootstrap' and is
safe to delete (defaults will be restored).`,
	}
	parent.AddCommand(
		configListCmd(&ConfigOptions{}),
		configGetCmd(&ConfigOptions{}),
		configSetCmd(&ConfigOptions{}),
		configPathCmd(&ConfigOptions{}),
		configEditCmd(&ConfigOptions{}),
	)
	return parent
}

func configListCmd(opts *ConfigOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config keys with current values and source",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunConfigList(opts)
		},
	}
}

func configGetCmd(opts *ConfigOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print the current value for one key",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return RunConfigGet(opts, args[0])
		},
	}
}

func configSetCmd(opts *ConfigOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a value to the config file",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return RunConfigSet(opts, args[0], args[1])
		},
	}
}

func configPathCmd(opts *ConfigOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the absolute config file path",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(opts.out(), p)
			return err
		},
	}
}

func configEditCmd(opts *ConfigOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config file in $EDITOR (creates it if missing)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunConfigEdit(opts)
		},
	}
}

// RunConfigList is the executable entry point for `config list` so tests
// can call it directly with an in-memory ConfigOptions.
func RunConfigList(opts *ConfigOptions) error {
	t := opts.theme()
	cfg, res, err := config.Load()
	if err != nil {
		return err
	}

	if res.FileExists {
		t.Info("config file: %s", res.Path)
	} else {
		t.Detail("no config file yet — showing built-in defaults (%s)", res.Path)
	}

	entries := config.Resolve(cfg, res)
	table := ui.Table{Headers: []string{"KEY", "VALUE", "SOURCE", "DESCRIPTION"}}
	for _, e := range entries {
		val := e.Value
		if val == "" {
			val = "(unset)"
		}
		table.Rows = append(table.Rows, []string{e.Key, val, string(e.Source), e.Description})
	}
	_, _ = fmt.Fprintln(opts.out())
	t.Render(table)
	_, _ = fmt.Fprintln(opts.out())
	t.Detail("change a value:  rallish config set <key> <value>")
	t.Detail("open in editor:  rallish config edit")
	return nil
}

// RunConfigGet prints the resolved value for one key.
func RunConfigGet(opts *ConfigOptions, key string) error {
	if !config.KnownKey(key) {
		return fmt.Errorf("unknown key %q (run `rallish config list` for valid keys)", key)
	}
	cfg, res, err := config.Load()
	if err != nil {
		return err
	}
	entries := config.Resolve(cfg, res)
	for _, e := range entries {
		if e.Key == key {
			_, err = fmt.Fprintln(opts.out(), e.Value)
			return err
		}
	}
	return fmt.Errorf("key %q not resolved", key)
}

// RunConfigSet writes a value and persists the file.
func RunConfigSet(opts *ConfigOptions, key, value string) error {
	t := opts.theme()
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, key, value); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	t.OK("set %s = %s", key, value)
	t.Detail("file: %s", path)
	return nil
}

// RunConfigEdit opens the config file in $EDITOR, creating it (with
// defaults) if it doesn't exist yet.
func RunConfigEdit(opts *ConfigOptions) error {
	t := opts.theme()
	path, err := config.Path()
	if err != nil {
		return err
	}

	// Ensure the file exists so the editor can save without needing
	// to create the parent directory itself.
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		cfg, _, lerr := config.Load()
		if lerr != nil {
			return lerr
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		t.Detail("created %s with defaults", path)
	}

	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	editor := chooseEditor(cfg.Editor)
	t.Info("opening %s in %s", path, editor)

	runner := opts.runEditor
	if runner == nil {
		runner = runEditorExec
	}
	return runner(editor, path)
}

// chooseEditor picks the editor binary: explicit config value > $VISUAL
// > $EDITOR > fallback to vi.
func chooseEditor(cfgValue string) string {
	if cfgValue != "" {
		return cfgValue
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return "vi"
}

func runEditorExec(editor, path string) error {
	cmd := exec.Command(editor, path) //nolint:gosec // editor selected from $EDITOR/$VISUAL/config
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
