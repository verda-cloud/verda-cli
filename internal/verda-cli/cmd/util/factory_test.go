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
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// newFactoryPreFlagParse mirrors production wiring: NewRootCommand builds the
// factory during command-tree construction, so Options carries zero values —
// --agent has not been parsed yet.
func newFactoryPreFlagParse() (Factory, *clioptions.Options) {
	opts := &clioptions.Options{Output: "table"}
	return NewFactory(opts, IOStreams{In: bytes.NewReader(nil), Out: io.Discard, ErrOut: io.Discard}), opts
}

// Regression: the factory is constructed before flag parsing, so an agent-mode
// decision taken in NewFactory reads Agent==false forever and every command
// keeps the interactive bubbletea prompter — `verda --agent <cmd>` then blocks
// on os.Stdin instead of failing fast. Prompter() must therefore resolve at
// call time, after flags land on the shared Options.
func TestFactory_PrompterResolvesAgentModeSetAfterConstruction(t *testing.T) {
	f, opts := newFactoryPreFlagParse()

	if _, isAgent := f.Prompter().(*agentPrompter); isAgent {
		t.Fatal("pre-parse factory returned agentPrompter; interactive mode would be broken")
	}

	opts.Agent = true // what flag parsing / opts.Complete() does

	if _, isAgent := f.Prompter().(*agentPrompter); !isAgent {
		t.Fatalf("Prompter() = %T after --agent was parsed, want *agentPrompter "+
			"(agent mode would block on stdin)", f.Prompter())
	}
}

// Every prompt entry point must fail fast with the documented structured error
// rather than reading stdin. Covers the whole Prompter surface because a
// command reaching *any* of these in agent mode is the hang.
func TestFactory_AgentModePromptsFailFast(t *testing.T) {
	f, opts := newFactoryPreFlagParse()
	opts.Agent = true
	p := f.Prompter()
	ctx := context.Background()

	calls := map[string]func() error{
		"Confirm":     func() (err error) { _, err = p.Confirm(ctx, "sure?"); return },
		"TextInput":   func() (err error) { _, err = p.TextInput(ctx, "name?"); return },
		"Password":    func() (err error) { _, err = p.Password(ctx, "secret?"); return },
		"Select":      func() (err error) { _, err = p.Select(ctx, "pick", []string{"a"}); return },
		"MultiSelect": func() (err error) { _, err = p.MultiSelect(ctx, "pick", []string{"a"}); return },
		"Editor":      func() (err error) { _, err = p.Editor(ctx, "edit"); return },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("prompt returned nil error in agent mode; caller would proceed as if answered")
			}
			var ae *AgentError
			if !errors.As(err, &ae) {
				t.Fatalf("error = %T (%v), want *AgentError", err, err)
			}
			if ae.Code != "INTERACTIVE_PROMPT_BLOCKED" {
				t.Errorf("code = %q, want INTERACTIVE_PROMPT_BLOCKED", ae.Code)
			}
		})
	}
}

// Interactive mode must keep the real prompter — guards against "fix" that
// returns agentPrompter unconditionally.
func TestFactory_InteractiveModeKeepsRealPrompter(t *testing.T) {
	f, _ := newFactoryPreFlagParse()
	if f.Prompter() == nil {
		t.Fatal("Prompter() = nil in interactive mode")
	}
	if _, isAgent := f.Prompter().(*agentPrompter); isAgent {
		t.Fatal("interactive mode returned agentPrompter")
	}
}

// Spinners write ANSI to the terminal and corrupt the agent stderr JSON
// channel; agent mode must have no Status regardless of output format.
func TestFactory_StatusSuppressedInAgentMode(t *testing.T) {
	tests := []struct {
		name      string
		agent     bool
		output    string
		wantNil   bool
		rationale string
	}{
		{"agent table", true, "table", true, "agent mode never renders a spinner"},
		{"agent json", true, "json", true, "agent mode never renders a spinner"},
		{"interactive json", false, "json", true, "non-table output is machine-consumed"},
		{"interactive table", false, "table", false, "humans get the spinner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &clioptions.Options{Output: tt.output, Agent: tt.agent}
			got := NewFactory(opts, IOStreams{In: bytes.NewReader(nil), Out: io.Discard, ErrOut: io.Discard}).Status()
			if (got == nil) != tt.wantNil {
				t.Errorf("Status() nil = %v, want %v (%s)", got == nil, tt.wantNil, tt.rationale)
			}
		})
	}
}

// Prompts are UI, not data (house rule): a factory built on piped streams must
// render prompt frames on ErrOut and leave Out clean for machine consumers.
// Backend-level split lives in pkg/tui/bubbletea (TestWithIO_*).
func TestFactory_PromptsRenderToErrOut(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	f := NewFactory(&clioptions.Options{Output: "table"}, IOStreams{
		In:     bytes.NewBufferString("\r"), // Enter: pick the first choice
		Out:    &out,
		ErrOut: &errOut,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	idx, err := f.Prompter().Select(ctx, "Pick one", []string{"alpha", "beta"}, tui.WithShowHints(true))
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if idx != 0 {
		t.Errorf("Select returned %d, want 0", idx)
	}
	if out.Len() != 0 {
		t.Errorf("prompt UI leaked into stdout: %q", out.String())
	}
	if errOut.Len() == 0 {
		t.Error("prompt rendered nowhere — ErrOut wiring lost")
	}
}

// Status.Table is data: it must land on Out even though prompts live on ErrOut.
func TestFactory_StatusTableWritesDataToOut(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	f := NewFactory(&clioptions.Options{Output: "table"}, IOStreams{
		In:     bytes.NewReader(nil),
		Out:    &out,
		ErrOut: &errOut,
	})

	status := f.Status()
	if status == nil {
		t.Fatal("Status() = nil in interactive table mode")
	}
	if err := status.Table(context.Background(), []string{"NAME"}, [][]string{{"row-1"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(out.String(), "NAME") {
		t.Errorf("table data missing from Out: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("table data polluted ErrOut: %q", errOut.String())
	}
}
