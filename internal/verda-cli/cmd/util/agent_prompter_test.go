// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
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

// The factory is constructed during command-tree building, before cobra parses
// flags and before opts.Complete() resolves VERDA_AGENT. Prompter() must
// therefore resolve the agent prompter lazily at call time (C2 regression test).
func TestFactoryPrompter_AgentSetAfterConstruction(t *testing.T) {
	opts := clioptions.NewOptions()
	f := NewFactory(opts, IOStreams{In: bytes.NewReader(nil), Out: io.Discard, ErrOut: io.Discard})

	opts.Agent = true

	_, err := f.Prompter().Confirm(context.Background(), "sure?")
	if err == nil {
		t.Fatal("expected INTERACTIVE_PROMPT_BLOCKED, got nil")
	}
	var ae *AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if ae.Code != "INTERACTIVE_PROMPT_BLOCKED" {
		t.Errorf("code = %q, want INTERACTIVE_PROMPT_BLOCKED", ae.Code)
	}
	if ae.ExitCode != ExitBadArgs {
		t.Errorf("exit code = %d, want %d", ae.ExitCode, ExitBadArgs)
	}

	if f.Status() != nil {
		t.Error("expected nil Status in agent mode (no spinners on the JSON surface)")
	}
}

func TestFactoryPrompter_InteractiveByDefault(t *testing.T) {
	opts := clioptions.NewOptions()
	f := NewFactory(opts, IOStreams{In: bytes.NewReader(nil), Out: io.Discard, ErrOut: io.Discard})

	if _, blocked := f.Prompter().(*agentPrompter); blocked {
		t.Error("non-agent factory must return the interactive prompter")
	}
}
