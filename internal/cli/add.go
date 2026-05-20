package cli

import (
	"context"
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/jazz1x/rallish/internal/preset"
	"github.com/jazz1x/rallish/internal/ui"
	"github.com/spf13/cobra"
)

//go:embed all:addassets/adapters
var adaptersFS embed.FS

type addAsset struct {
	Name        string
	Type        string
	Description string
}

// AddOptions holds flags for the add command.
type AddOptions struct {
	Global bool
	From   string
	Yes    bool

	// test hooks
	stdout io.Writer
	stdin  io.Reader
}

func (o *AddOptions) out() io.Writer {
	if o.stdout != nil {
		return o.stdout
	}
	return os.Stdout
}

// theme constructs a ui.Theme bound to the options' streams. Tests
// inject buffers via stdout/stdin and get plain output for free.
func (o *AddOptions) theme() *ui.Theme {
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

// AddCmd returns the `add` cobra command.
func AddCmd() *cobra.Command {
	var opts AddOptions
	cmd := &cobra.Command{
		Use:   "add [type] [name]",
		Short: "Install adapters, presets, or skills",
		Long: `Install rallish components.

Supported types: adapter, preset, skill.

Examples:
  rallish add                                  # interactive picker
  rallish add --list                           # list built-ins
  rallish add preset solo-ralph                # install a built-in preset
  rallish add adapter claude --global          # install for all projects
  rallish add preset my-review --from URL      # install from a URL`,
		Args: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list && len(args) > 0 {
				return fmt.Errorf("--list does not take arguments")
			}
			if !list && len(args) != 0 && len(args) != 2 {
				return fmt.Errorf("requires 0 args (interactive) or 2 args (type name)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list {
				return listComponents(opts.out())
			}
			if len(args) == 0 {
				return runAddInteractive(cmd.Context(), opts)
			}
			return runAdd(cmd.Context(), opts, args)
		},
	}
	cmd.Flags().BoolVar(&opts.Global, "global", false, "Install to ~/.rallish instead of ./.rallish")
	cmd.Flags().StringVar(&opts.From, "from", "", "Download component from a URL")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().Bool("list", false, "List available built-in components")
	return cmd
}

// runAddInteractive walks the user through picking a type and name
// from the built-in catalog. Used when `rallish add` is invoked with
// no positional arguments.
func runAddInteractive(ctx context.Context, opts AddOptions) error {
	t := opts.theme()

	t.Heading("rallish add — pick a component to install")
	t.Detail("Press enter to accept the highlighted choice. Ctrl-C to abort.")

	typeChoices := []string{"preset", "adapter"}
	typ, _, err := t.SelectOne("Component type?", typeChoices, 0)
	if err != nil {
		return err
	}

	all, err := loadAllBuiltIns()
	if err != nil {
		return err
	}
	var names []string
	for _, a := range all {
		if a.Type == typ {
			names = append(names, a.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no built-in %ss found", typ)
	}

	picked, _, err := t.SelectOne(fmt.Sprintf("Which %s?", typ), names, 0)
	if err != nil {
		return err
	}

	scope, _, err := t.SelectOne("Install scope?",
		[]string{"local (./.rallish)", "global (~/.rallish)"}, 0)
	if err != nil {
		return err
	}
	opts.Global = strings.HasPrefix(scope, "global")

	return runAdd(ctx, opts, []string{typ, picked})
}

func runAdd(ctx context.Context, opts AddOptions, args []string) error {
	t := opts.theme()
	typ := args[0]
	name := args[1]

	if err := validateType(typ); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	targetDir, err := resolveTargetDir(typ, opts.Global)
	if err != nil {
		return err
	}
	targetPath := filepath.Join(targetDir, name+".yaml")

	var data []byte
	var usage string

	if opts.From != "" {
		t.Info("Download from %s", opts.From)
		data, err = downloadURL(ctx, opts.From)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
	} else {
		asset, err := findBuiltIn(typ, name)
		if err != nil {
			return err
		}
		if asset == nil {
			return fmt.Errorf("no built-in %s named %q (use --from to install from a URL)", typ, name)
		}
		t.Info("Found built-in %s: %s", typ, name)
		if asset.Description != "" {
			t.Detail("%s", asset.Description)
		}

		data, err = loadBuiltInData(typ, name)
		if err != nil {
			return err
		}
	}

	exists := false
	if _, err := os.Stat(targetPath); err == nil {
		exists = true
	}

	if !opts.Yes {
		var msg string
		if exists {
			msg = fmt.Sprintf("Overwrite %s?", targetPath)
		} else {
			msg = fmt.Sprintf("Install to %s?", targetPath)
		}
		ok, err := t.Confirm(msg, true)
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(opts.out(), "Cancelled.")
			return nil
		}
	}

	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(targetPath, data, 0o644); err != nil { //nolint:gosec // user-installed config file
		return fmt.Errorf("write file: %w", err)
	}

	switch typ {
	case "preset":
		usage = fmt.Sprintf("rallish squash --preset %s", name)
	case "adapter":
		usage = fmt.Sprintf("Use runtime %q in your presets", name)
	case "skill":
		usage = fmt.Sprintf("Skill %q installed", name)
	}

	t.OK("Installed %s %s to %s", typ, name, targetPath)
	if usage != "" {
		t.Detail("Usage: %s", usage)
	}
	return nil
}

func validateType(typ string) error {
	switch typ {
	case "adapter", "preset", "skill":
		return nil
	default:
		return fmt.Errorf("unknown type %q: must be adapter, preset, or skill", typ)
	}
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	return nil
}

func resolveTargetDir(typ string, global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, ".rallish", typ+"s"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return filepath.Join(cwd, ".rallish", typ+"s"), nil
}

func listComponents(w io.Writer) error {
	rows, err := loadAllBuiltIns()
	if err != nil {
		return err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Name < rows[j].Name
	})

	t := ui.New()
	t.Out = w
	t.ErrOut = w
	// Detect color from the underlying writer (still works for *os.File).
	t.Color = false
	if f, ok := w.(*os.File); ok {
		stat, err := f.Stat()
		if err == nil && stat.Mode()&os.ModeCharDevice != 0 {
			if _, no := os.LookupEnv("NO_COLOR"); !no && os.Getenv("TERM") != "dumb" {
				t.Color = true
			}
		}
	}

	table := ui.Table{Headers: []string{"TYPE", "NAME", "DESCRIPTION"}}
	for _, r := range rows {
		table.Rows = append(table.Rows, []string{r.Type, r.Name, r.Description})
	}
	t.Render(table)
	return nil
}

func loadAllBuiltIns() ([]addAsset, error) {
	var rows []addAsset
	presets, err := preset.LoadEmbedded()
	if err != nil {
		return nil, fmt.Errorf("load presets: %w", err)
	}
	for name, p := range presets {
		rows = append(rows, addAsset{Name: name, Type: "preset", Description: p.Description})
	}
	adapters, err := loadAdapters()
	if err != nil {
		return nil, fmt.Errorf("load adapters: %w", err)
	}
	for name, a := range adapters {
		rows = append(rows, addAsset{Name: name, Type: "adapter", Description: a.Description})
	}
	return rows, nil
}

func findBuiltIn(typ, name string) (*addAsset, error) {
	switch typ {
	case "preset":
		presets, err := preset.LoadEmbedded()
		if err != nil {
			return nil, err
		}
		p, ok := presets[name]
		if !ok {
			return nil, nil
		}
		return &addAsset{Name: name, Type: "preset", Description: p.Description}, nil
	case "adapter":
		adapters, err := loadAdapters()
		if err != nil {
			return nil, err
		}
		a, ok := adapters[name]
		if !ok {
			return nil, nil
		}
		return &addAsset{Name: name, Type: "adapter", Description: a.Description}, nil
	case "skill":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}

func loadBuiltInData(typ, name string) ([]byte, error) {
	switch typ {
	case "preset":
		return preset.ReadRaw(name)
	case "adapter":
		return adaptersFS.ReadFile(path.Join("addassets", "adapters", name+".yaml"))
	default:
		return nil, fmt.Errorf("no built-in data for type %q", typ)
	}
}

func loadAdapters() (map[string]addAsset, error) {
	entries, err := adaptersFS.ReadDir("addassets/adapters")
	if err != nil {
		return nil, fmt.Errorf("read adapters dir: %w", err)
	}

	out := make(map[string]addAsset)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := adaptersFS.ReadFile(path.Join("addassets", "adapters", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read adapter %q: %w", e.Name(), err)
		}
		var m struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse adapter %q: %w", e.Name(), err)
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		out[name] = addAsset{Name: m.Name, Type: "adapter", Description: m.Description}
	}
	return out, nil
}

func downloadURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
