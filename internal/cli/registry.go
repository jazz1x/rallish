package cli

import (
	"fmt"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/adapter/claude"
	"github.com/jazz1x/rallish/internal/adapter/fake"
	"github.com/jazz1x/rallish/internal/adapter/kimi"
)

// BuildRegistry creates a registry with all available adapters.
func BuildRegistry() (*adapter.Registry, error) {
	reg := adapter.NewRegistry()

	if c, err := claude.New(); err == nil {
		if err := reg.Register("claude", c); err != nil {
			return nil, fmt.Errorf("register claude: %w", err)
		}
	}
	if k, err := kimi.New(); err == nil {
		if err := reg.Register("kimi", k); err != nil {
			return nil, fmt.Errorf("register kimi: %w", err)
		}
	}
	if err := reg.Register("fake", fake.NewPingPong(5, 1*time.Second)); err != nil {
		return nil, fmt.Errorf("register fake: %w", err)
	}

	return reg, nil
}
