package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jazz1x/rallish/internal/config"
	"github.com/jazz1x/rallish/internal/doctor"
	"github.com/jazz1x/rallish/internal/ui"
	"github.com/spf13/cobra"
)

// DoctorOptions controls the human-facing `rallish doctor` view.
type DoctorOptions struct {
	stdout io.Writer
	probe  bool
}

// DoctorCmd returns the `doctor` cobra command.
func DoctorCmd() *cobra.Command {
	var opts DoctorOptions
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose adapters, daemon, and config",
		Long: `Run health checks and print a status table:
  - daemon reachability (~/.rallish/socket or ~/.rallish/port)
  - adapter availability (claude, kimi, ...)
  - config file path

A non-running daemon is fine — it auto-spawns on the first rally.

With --probe, each adapter on PATH is asked to answer one minimal real turn so
auth is verified (not just install). This spends one cheap turn per adapter.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunDoctor(cmd.Context(), opts)
		},
	}
	c.Flags().BoolVar(&opts.probe, "probe", false, "verify adapter auth with a live minimal turn (spends one turn per adapter)")
	return c
}

// RunDoctor is the exported entry point used by both the cobra command
// and tests.
func RunDoctor(ctx context.Context, opts DoctorOptions) error {
	w := opts.stdout
	if w == nil {
		w = os.Stdout
	}
	t := ui.New()
	t.Out = w
	t.ErrOut = w
	if _, ok := w.(*os.File); !ok {
		t.Color = false
	}

	home, _ := os.UserHomeDir()
	var inspectOpts []doctor.Option
	if opts.probe {
		inspectOpts = append(inspectOpts, doctor.WithProbe())
	}
	rep, err := doctor.Inspect(ctx, home, inspectOpts...)
	if err != nil {
		t.Err("%v", err)
		return err
	}

	t.Heading("rallish doctor")
	for _, c := range rep.Checks {
		renderCheck(t, c)
	}
	_, _ = fmt.Fprintln(w)
	t.Detail("config:  %s", configPathOrDefault())
	t.Detail("skill:   %s/.claude/skills/rallish", home)
	return nil
}

func renderCheck(t *ui.Theme, c doctor.Check) {
	label := fmt.Sprintf("%-18s — %s", c.Name, c.Detail)
	switch c.Status {
	case doctor.StatusOK:
		t.OK("%s", label)
	case doctor.StatusFail:
		t.Err("%s", label)
	case doctor.StatusWarn:
		t.Warn("%s", label)
	default:
		t.Info("%s", label)
	}
}

func configPathOrDefault() string {
	if p, err := config.Path(); err == nil {
		return p
	}
	return "~/.rallish/config.yaml"
}
