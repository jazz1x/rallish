// Package doctor checks adapters, paths, and permissions.
package doctor

import (
	"context"
	"log/slog"

	"github.com/jazz1x/rallish/internal/adapter/claude"
	"github.com/jazz1x/rallish/internal/adapter/kimi"
)

type pathReporter interface {
	Path() string
}

// Run performs health checks for all known adapters.
func Run(ctx context.Context) error {
	_ = ctx
	logger := slog.Default()

	type candidate struct {
		name    string
		checker interface {
			Check() error
			pathReporter
		}
		envList []string
	}

	var candidates []candidate

	if c, err := claude.New(); err != nil {
		logger.Info("adapter not found", "name", "claude", "error", err)
	} else {
		candidates = append(candidates, candidate{
			name:    c.Name(),
			checker: c,
			envList: []string{"PATH", "HOME", "LANG", "TERM", "ANTHROPIC_*"},
		})
	}

	if k, err := kimi.New(); err != nil {
		logger.Info("adapter not found", "name", "kimi", "error", err)
	} else {
		candidates = append(candidates, candidate{
			name:    k.Name(),
			checker: k,
			envList: []string{"PATH", "HOME", "LANG", "TERM", "KIMI_*"},
		})
	}

	for _, c := range candidates {
		if err := c.checker.Check(); err != nil {
			logger.Error("adapter check failed", "name", c.name, "error", err)
		} else {
			logger.Info("adapter check passed", "name", c.name)
		}
		logger.Info("adapter path", "name", c.name, "path", c.checker.Path())
		logger.Info("adapter env allowlist", "name", c.name, "allowlist", c.envList)
	}

	return nil
}
