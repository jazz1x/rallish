package cli

import (
	"bufio"
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

func (o *AddOptions) in() io.Reader {
	if o.stdin != nil {
		return o.stdin
	}
	return os.Stdin
}

// AddCmd returns the `add` cobra command.
func AddCmd() *cobra.Command {
	var opts AddOptions
	cmd := &cobra.Command{
		Use:   "add <type> <name>",
		Short: "Install adapters, presets, or skills",
		Long: `Install rallish components.

Supported types: adapter, preset, skill.

Examples:
  rallish add --list
  rallish add preset solo-ralph
  rallish add adapter claude --global
  rallish add preset my-review --from https://example.com/presets/my-review.yaml`,
		Args: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list {
				if len(args) > 0 {
					return fmt.Errorf("--list does not take arguments")
				}
				return nil
			}
			if len(args) != 2 {
				return fmt.Errorf("requires exactly 2 arguments (type and name)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list {
				return listComponents(opts.out())
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

func runAdd(ctx context.Context, opts AddOptions, args []string) error {
	w := opts.out()
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
		_, _ = fmt.Fprintf(w, "→ Download from %s\n", opts.From)
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

		_, _ = fmt.Fprintf(w, "→ Found built-in %s: %s\n", typ, name)
		if asset.Description != "" {
			_, _ = fmt.Fprintf(w, "  Description: %s\n", asset.Description)
		}

		data, err = loadBuiltInData(typ, name)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(targetPath); err == nil {
		if !opts.Yes {
			ok, err := confirm(w, opts.in(), fmt.Sprintf("→ Overwrite %s?", targetPath))
			if err != nil {
				return err
			}
			if !ok {
				_, _ = fmt.Fprintln(w, "Cancelled.")
				return nil
			}
		}
	} else {
		if !opts.Yes {
			ok, err := confirm(w, opts.in(), fmt.Sprintf("→ Install to %s?", targetPath))
			if err != nil {
				return err
			}
			if !ok {
				_, _ = fmt.Fprintln(w, "Cancelled.")
				return nil
			}
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
		usage = fmt.Sprintf("rallish start --preset %s", name)
	case "adapter":
		usage = fmt.Sprintf("Use runtime %q in your presets", name)
	case "skill":
		usage = fmt.Sprintf("Skill %q installed", name)
	}

	_, _ = fmt.Fprintf(w, "✓ Installed %s %s to %s\n", typ, name, targetPath)
	if usage != "" {
		_, _ = fmt.Fprintf(w, "  Usage: %s\n", usage)
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
	var rows []addAsset

	// List presets
	presets, err := preset.LoadEmbedded()
	if err != nil {
		return fmt.Errorf("load presets: %w", err)
	}
	for name, p := range presets {
		rows = append(rows, addAsset{
			Name:        name,
			Type:        "preset",
			Description: p.Description,
		})
	}

	// List adapters
	adapters, err := loadAdapters()
	if err != nil {
		return fmt.Errorf("load adapters: %w", err)
	}
	for name, a := range adapters {
		rows = append(rows, addAsset{
			Name:        name,
			Type:        "adapter",
			Description: a.Description,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Name < rows[j].Name
	})

	_, _ = fmt.Fprintln(w, "TYPE      NAME         DESCRIPTION")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%-9s %-12s %s\n", r.Type, r.Name, r.Description)
	}
	return nil
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
		return &addAsset{
			Name:        name,
			Type:        "preset",
			Description: p.Description,
		}, nil
	case "adapter":
		adapters, err := loadAdapters()
		if err != nil {
			return nil, err
		}
		a, ok := adapters[name]
		if !ok {
			return nil, nil
		}
		return &addAsset{
			Name:        name,
			Type:        "adapter",
			Description: a.Description,
		}, nil
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
		out[name] = addAsset{
			Name:        m.Name,
			Type:        "adapter",
			Description: m.Description,
		}
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

func confirm(w io.Writer, r io.Reader, msg string) (bool, error) {
	_, _ = fmt.Fprintf(w, "%s [Y/n] ", msg)
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			if line == "" {
				return false, nil
			}
		} else {
			return false, err
		}
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return true, nil
	}
	return strings.ToLower(line) == "y" || strings.ToLower(line) == "yes", nil
}
