package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jazz1x/rallish/pkg/contract"
)

var jsonBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// BuildPrompt creates a deterministic prompt embedding the TurnRequest.
func BuildPrompt(req contract.TurnRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	modelHint := ""
	if req.ModelHint != "" {
		modelHint = fmt.Sprintf("\nModel hint: use model %q for this turn.\n", req.ModelHint)
	}
	return fmt.Sprintf(`You are a turn-taking agent in a multi-agent system.%s

The following JSON is your TurnRequest:

`+"```json\n%s\n```"+`

Respond with a JSON object conforming to the TurnResponse schema, wrapped in a fenced code block.

TurnResponse fields:
- done (bool)
- handoff_to (string, optional)
- summary (string)
- artifacts (array of strings, optional)
- self_eval (string: confident, uncertain, blocked)
- notes_for_human (string, optional)
- usage (object with tokens_in, tokens_out, ms, optional)

Do not include any text outside the fenced JSON block.`, modelHint, string(data)), nil
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
