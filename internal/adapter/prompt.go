package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jazz1x/rallish/pkg/contract"
)

var jsonBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// promptRequest is a slimmed-down view of TurnRequest containing only the
// fields the model needs to reason about its turn.
type promptRequest struct {
	Turn        int                `json:"turn"`
	Role        string             `json:"role"`
	RuntimeHint string             `json:"runtime_hint"`
	Budget      contract.Budget    `json:"budget"`
	LastTurn    *contract.LastTurn `json:"last_turn,omitempty"`
	Task        contract.Task      `json:"task"`
	Scratch     string             `json:"scratch,omitempty"`
}

// BuildPrompt creates a deterministic prompt embedding the TurnRequest.
func BuildPrompt(req contract.TurnRequest) (string, error) {
	pr := promptRequest{
		Turn:        req.Turn,
		Role:        req.Role,
		RuntimeHint: req.RuntimeHint,
		Budget:      req.Budget,
		LastTurn:    req.LastTurn,
		Task:        req.Task,
		Scratch:     req.Scratch,
	}
	data, err := json.Marshal(pr)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.Grow(512 + len(data))
	b.WriteString("You are a turn-taking agent in a multi-agent system.")
	if req.ModelHint != "" {
		fmt.Fprintf(&b, "\nModel hint: use model %q for this turn.\n", req.ModelHint)
	}
	if req.Scratch != "" {
		fmt.Fprintf(&b, "\n\nShared scratchpad (read-only context from prior turns):\n\n%s\n", req.Scratch)
	}
	if req.LastTurn != nil {
		if req.LastTurn.Intent == contract.HandoffIntentCrossCheck {
			fmt.Fprintf(&b, "\n\nHandoff intent: CROSS-CHECK the previous turn. Read the listed Artifacts and the original Task fresh. Do not accept the previous Summary at face value; look for mistakes, gaps, and contradictions.\n")
		} else {
			fmt.Fprintf(&b, "\n\nHandoff intent: CONTINUE the previous turn. Use the previous Summary as working state; Artifacts are context for your next step.\n")
		}
	}
	b.WriteString("\n\nThe following JSON is your TurnRequest:\n\n```json\n")
	b.Write(data)
	b.WriteString("\n```\n\nRespond with a JSON object conforming to the TurnResponse schema, wrapped in a fenced code block.\n\nTurnResponse fields:\n- done (bool)\n- handoff_to (string, optional)\n- handoff_intent (string: continue or cross_check, optional)\n- summary (string)\n- artifacts (array of strings, optional)\n- self_eval (string: confident, uncertain, blocked)\n- notes_for_human (string, optional)\n- usage (object with tokens_in, tokens_out, ms, optional)\n- claims (array of violations, optional)\n\nDo not include any text outside the fenced JSON block.")
	return b.String(), nil
}

// ParseLastJSONBlock extracts a TurnResponse from CLI output. It prefers the
// last fenced JSON block; if no fence is present (some CLIs strip them), it
// falls back to scanning for the last balanced {...} object that decodes into
// the response schema.
func ParseLastJSONBlock(out []byte, resp *contract.TurnResponse) error {
	if matches := jsonBlockRegex.FindAllSubmatch(out, -1); len(matches) > 0 {
		last := matches[len(matches)-1][1]
		if err := json.Unmarshal(last, resp); err != nil {
			return fmt.Errorf("unmarshaling TurnResponse: %w", err)
		}
		return nil
	}
	if obj, ok := lastBalancedObject(out); ok {
		if err := json.Unmarshal(obj, resp); err == nil {
			return nil
		}
	}
	return errors.New("no JSON TurnResponse found in output")
}

// lastBalancedObject scans for the last brace-balanced JSON object in s. It
// only counts braces outside of string literals so embedded "{" inside strings
// don't confuse the depth counter.
func lastBalancedObject(s []byte) ([]byte, bool) {
	var best []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth := 0
		inStr := false
		esc := false
		for j := i; j < len(s); j++ {
			c := s[j]
			if inStr {
				if esc {
					esc = false
					continue
				}
				if c == '\\' {
					esc = true
					continue
				}
				if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					best = s[i : j+1]
					i = j
				}
			}
			if depth == 0 && c == '}' {
				break
			}
		}
	}
	return best, best != nil
}
