package util

import (
	"context"

	"github.com/verda-cloud/verdagostack/pkg/tui"
)

// agentPrompter implements tui.Prompter but returns structured errors for every
// prompt method. This prevents commands from blocking on interactive input when
// running in --agent mode.
type agentPrompter struct{}

var _ tui.Prompter = (*agentPrompter)(nil)

func (p *agentPrompter) Confirm(_ context.Context, prompt string, _ ...tui.ConfirmOption) (bool, error) {
	return false, NewPromptBlockedError("confirm", prompt, nil)
}

func (p *agentPrompter) TextInput(_ context.Context, prompt string, _ ...tui.TextInputOption) (string, error) {
	return "", NewPromptBlockedError("text_input", prompt, nil)
}

func (p *agentPrompter) Password(_ context.Context, prompt string) (string, error) {
	return "", NewPromptBlockedError("password", prompt, nil)
}

func (p *agentPrompter) Select(_ context.Context, prompt string, choices []string, _ ...tui.SelectOption) (int, error) {
	return -1, NewPromptBlockedError("select", prompt, choices)
}

func (p *agentPrompter) MultiSelect(_ context.Context, prompt string, choices []string, _ ...tui.MultiSelectOption) ([]int, error) {
	return nil, NewPromptBlockedError("multi_select", prompt, choices)
}

func (p *agentPrompter) Editor(_ context.Context, prompt string, _ ...tui.EditorOption) (string, error) {
	return "", NewPromptBlockedError("editor", prompt, nil)
}
