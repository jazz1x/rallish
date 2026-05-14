// Package preset loads and validates hocketty preset YAML with strict schema checking.
package preset

import (
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
	"github.com/jazz1x/hocketty/pkg/contract"
)

var validRouting = map[string]bool{
	"round_robin":              true,
	"handoff_then_round_robin": true,
	"strict_round_robin":       true,
	"last_writer_wins":         true,
}

type yamlPreset struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Roles       []yamlRole  `yaml:"roles"`
	Routing     string      `yaml:"routing"`
	Budget      yamlBudget  `yaml:"budget"`
	ExitWhen    []string    `yaml:"exit_when"`
	Scratch     yamlScratch `yaml:"scratch"`
}

type yamlRole struct {
	ID      string `yaml:"id"`
	Runtime string `yaml:"runtime"`
	Model   string `yaml:"model"`
}

type yamlBudget struct {
	MaxTurns        int   `yaml:"max_turns"`
	MaxTokens       int64 `yaml:"max_tokens"`
	DeadlineMinutes int   `yaml:"deadline_minutes"`
}

type yamlScratch struct {
	MaxKB         int64  `yaml:"max_kb"`
	SummarizeWith string `yaml:"summarize_with"`
}

// Load reads a preset from r and validates it. Unknown YAML keys are rejected.
func Load(r io.Reader) (contract.Preset, error) {
	var yp yamlPreset
	dec := yaml.NewDecoder(r, yaml.DisallowUnknownField())
	if err := dec.Decode(&yp); err != nil {
		return contract.Preset{}, fmt.Errorf("decode preset: %w", err)
	}

	if yp.Name == "" {
		return contract.Preset{}, fmt.Errorf("preset name is required")
	}
	if len(yp.Roles) == 0 {
		return contract.Preset{}, fmt.Errorf("preset must define at least one role")
	}
	if yp.Budget.MaxTurns <= 0 {
		return contract.Preset{}, fmt.Errorf("budget.max_turns must be > 0")
	}
	if yp.Budget.MaxTokens <= 0 {
		return contract.Preset{}, fmt.Errorf("budget.max_tokens must be > 0")
	}
	if !validRouting[yp.Routing] {
		return contract.Preset{}, fmt.Errorf("invalid routing %q", yp.Routing)
	}

	p := contract.Preset{
		Name:        yp.Name,
		Description: yp.Description,
		Routing:     yp.Routing,
		Budget: contract.Budget{
			TokensLeft: yp.Budget.MaxTokens,
			TurnsLeft:  yp.Budget.MaxTurns,
			DeadlineMS: int64(yp.Budget.DeadlineMinutes) * 60000,
		},
		Scratch: contract.ScratchConfig{
			MaxKB:         yp.Scratch.MaxKB,
			SummarizeWith: yp.Scratch.SummarizeWith,
		},
	}

	for _, yr := range yp.Roles {
		p.Roles = append(p.Roles, contract.Role{
			ID:      yr.ID,
			Runtime: yr.Runtime,
			Model:   yr.Model,
		})
	}

	for _, ec := range yp.ExitWhen {
		p.ExitWhen = append(p.ExitWhen, contract.ExitCondition(ec))
	}

	return p, nil
}
