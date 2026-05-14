package cli

import (
	"fmt"

	"github.com/jazz1x/hocketty/internal/adapter"
	"github.com/jazz1x/hocketty/internal/adapter/fake"
	"github.com/jazz1x/hocketty/pkg/contract"
)

// BuildRegistry creates a registry with all available adapters.
func BuildRegistry() (*adapter.Registry, error) {
	reg := adapter.NewRegistry()

	turnNum := 0
	fakeAd := fake.New(func(_ int) contract.TurnResponse {
		turnNum++
		return contract.TurnResponse{
			Done:     turnNum >= 2,
			Summary:  "ack",
			SelfEval: contract.SelfEvalConfident,
		}
	})

	if err := reg.Register("claude", fakeAd); err != nil {
		return nil, fmt.Errorf("register claude: %w", err)
	}
	if err := reg.Register("kimi", fakeAd); err != nil {
		return nil, fmt.Errorf("register kimi: %w", err)
	}
	if err := reg.Register("fake", fake.New(nil)); err != nil {
		return nil, fmt.Errorf("register fake: %w", err)
	}

	return reg, nil
}
