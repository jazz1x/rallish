package ui

import (
	"fmt"
	"io"
	"strings"
)

// Ask prints a [◆] prompt question and returns the trimmed user input.
// An empty answer returns the supplied default. Pass an empty default
// to require a non-empty answer.
func (t *Theme) Ask(question, def string) (string, error) {
	openP, closeP := t.styleANSI(StylePrompt)
	openD, closeD := t.styleANSI(StyleDim)
	hint := ""
	if def != "" {
		hint = fmt.Sprintf(" %s(%s)%s", openD, def, closeD)
	}
	fmt.Fprintf(t.out(), "%s%s%s  %s%s ", openP, GlyphPrompt, closeP, question, hint)

	line, err := t.inputReader().readLine()
	if err != nil && err != io.EOF {
		return "", err
	}
	ans := strings.TrimSpace(line)
	if ans == "" {
		return def, nil
	}
	return ans, nil
}

// Confirm prints a [◆] yes/no prompt and returns the user's choice.
// defaultYes selects the highlighted option on bare enter.
func (t *Theme) Confirm(question string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	openP, closeP := t.styleANSI(StylePrompt)
	openD, closeD := t.styleANSI(StyleDim)
	fmt.Fprintf(t.out(), "%s%s%s  %s %s(%s)%s ", openP, GlyphPrompt, closeP, question, openD, hint, closeD)

	line, err := t.inputReader().readLine()
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans == "" {
		return defaultYes, nil
	}
	return ans == "y" || ans == "yes", nil
}

// SelectOne presents a numbered list of choices and returns the picked
// label. When stdin is non-interactive (or only EOF arrives) the first
// option is returned to keep automation flows unblocked.
func (t *Theme) SelectOne(question string, choices []string, defaultIdx int) (string, int, error) {
	if len(choices) == 0 {
		return "", -1, fmt.Errorf("no choices")
	}
	if defaultIdx < 0 || defaultIdx >= len(choices) {
		defaultIdx = 0
	}

	openP, closeP := t.styleANSI(StylePrompt)
	openD, closeD := t.styleANSI(StyleDim)
	fmt.Fprintf(t.out(), "%s%s%s  %s\n", openP, GlyphPrompt, closeP, question)
	for i, c := range choices {
		marker := " "
		if i == defaultIdx {
			marker = "›"
		}
		fmt.Fprintf(t.out(), "  %s %d) %s\n", marker, i+1, c)
	}
	fmt.Fprintf(t.out(), "%s  pick (1-%d, enter=%d):%s ", openD, len(choices), defaultIdx+1, closeD)

	line, err := t.inputReader().readLine()
	if err != nil && err != io.EOF {
		return "", -1, err
	}
	ans := strings.TrimSpace(line)
	if ans == "" {
		return choices[defaultIdx], defaultIdx, nil
	}
	var idx int
	if _, err := fmt.Sscanf(ans, "%d", &idx); err != nil {
		return "", -1, fmt.Errorf("not a number: %q", ans)
	}
	if idx < 1 || idx > len(choices) {
		return "", -1, fmt.Errorf("out of range: %d", idx)
	}
	return choices[idx-1], idx - 1, nil
}
