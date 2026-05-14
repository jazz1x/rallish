package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jazz1x/hocketty/pkg/contract"
)

var jsonBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// BuildPrompt creates a deterministic prompt embedding the TurnRequest.
func BuildPrompt(req contract.TurnRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`You are a turn-taking agent in a multi-agent system.

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

Do not include any text outside the fenced JSON block.`, string(data)), nil
}

// ParseLastJSONBlock extracts the last fenced JSON block from output and unmarshals it into resp.
func ParseLastJSONBlock(out []byte, resp *contract.TurnResponse) error {
	matches := jsonBlockRegex.FindAllSubmatch(out, -1)
	if len(matches) == 0 {
		return errors.New("no fenced JSON block found in output")
	}
	last := matches[len(matches)-1][1]
	if err := json.Unmarshal(last, resp); err != nil {
		return fmt.Errorf("unmarshaling TurnResponse: %w", err)
	}
	return nil
}
