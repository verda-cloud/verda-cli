package util

import (
	"context"
	"errors"
	"testing"
)

func TestAgentPrompter_ReturnsAgentError(t *testing.T) {
	p := &agentPrompter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Confirm", func() (err error) { _, err = p.Confirm(ctx, "sure?"); return }},
		{"TextInput", func() (err error) { _, err = p.TextInput(ctx, "name?"); return }},
		{"Password", func() (err error) { _, err = p.Password(ctx, "secret?"); return }},
		{"Select", func() (err error) { _, err = p.Select(ctx, "pick", []string{"a", "b"}); return }},
		{"MultiSelect", func() (err error) { _, err = p.MultiSelect(ctx, "pick many", []string{"a", "b"}); return }},
		{"Editor", func() (err error) { _, err = p.Editor(ctx, "edit"); return }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var ae *AgentError
			if !errors.As(err, &ae) {
				t.Fatalf("expected *AgentError, got %T: %v", err, err)
			}
			if ae.Code != "INTERACTIVE_PROMPT_BLOCKED" {
				t.Errorf("code = %q, want INTERACTIVE_PROMPT_BLOCKED", ae.Code)
			}
		})
	}
}
