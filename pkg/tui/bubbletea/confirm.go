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

package bubbletea

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

type confirmModel struct {
	prompt      string
	value       bool
	decided     bool
	aborted     bool
	interrupted bool // true for Ctrl+C (hard cancel), false for Esc (soft cancel)
	bindings    []KeyBinding[confirmModel]
}

// DefaultConfirmBindings returns a fresh copy of the canonical
// binding set. Stable IDs: yes-no, confirm, esc, exit.
func DefaultConfirmBindings() []KeyBinding[confirmModel] {
	return []KeyBinding[confirmModel]{
		{
			ID:    "yes-no",
			Match: MatchText(),
			Label: func(*confirmModel) string { return "y/n" },
			Handle: func(m *confirmModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				switch msg.Text {
				case "y", "Y":
					m.value = true
					m.decided = true
					return tea.Quit, true
				case "n", "N":
					m.value = false
					m.decided = true
					return tea.Quit, true
				}
				return nil, false // unrecognized printable; no later binding claims it
			},
		},
		{
			ID:    "confirm",
			Match: MatchKey(tea.KeyEnter),
			Label: func(*confirmModel) string { return "enter confirm" },
			Handle: func(m *confirmModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.decided = true
				return tea.Quit, true
			},
		},
		{
			ID:    "esc",
			Match: MatchKey(tea.KeyEscape),
			Label: func(*confirmModel) string { return hintEscBack },
			Handle: func(m *confirmModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.aborted = true
				return func() tea.Msg { return GoBackMsg{} }, true
			},
		},
		{
			ID:    "exit",
			Match: MatchRune('c', tea.ModCtrl),
			Label: func(*confirmModel) string { return hintCtrlCExit },
			Handle: func(m *confirmModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.interrupted = true
				return tea.Quit, true
			},
		},
	}
}

func newConfirmModel(prompt string, cfg tui.ConfirmConfig) confirmModel {
	defaults := ApplyBindingOverrides(DefaultConfirmBindings(), cfg.RelabelByID, cfg.HiddenByID)
	var bindings []KeyBinding[confirmModel]
	if extras, ok := cfg.ExtraBindings.([]KeyBinding[confirmModel]); ok && len(extras) > 0 {
		bindings = make([]KeyBinding[confirmModel], 0, len(extras)+len(defaults))
		bindings = append(bindings, extras...)
		bindings = append(bindings, defaults...)
	} else {
		bindings = defaults
	}
	return confirmModel{
		prompt:   prompt,
		value:    cfg.Default,
		bindings: bindings,
	}
}

// WithConfirmAddBindings prepends extras so they outrank the default
// catch-all matchers. See WithSelectAddBindings for semantics.
func WithConfirmAddBindings(extras ...KeyBinding[confirmModel]) tui.ConfirmOption {
	return func(c *tui.ConfirmConfig) {
		existing, _ := c.ExtraBindings.([]KeyBinding[confirmModel])
		c.ExtraBindings = append(existing, extras...)
	}
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(GoBackMsg); ok {
		return m, tea.Quit // standalone mode quit; wizard composite intercepts before this
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	cmd, _ := Dispatch(&m, m.bindings, key)
	return m, cmd
}

// Hints derives key hints from the resolved bindings.
func (m confirmModel) Hints() []string {
	return HintsFor(&m, m.bindings)
}

// Result returns the confirm value after the user decides.
func (m confirmModel) Result() (any, bool) {
	return m.value, m.decided
}

// NewConfirmPrompt creates a confirm prompt model for use in the wizard composite.
func NewConfirmPrompt(prompt string, cfg tui.ConfirmConfig) PromptModel {
	return newConfirmModel(prompt, cfg)
}

func (m confirmModel) View() tea.View {
	hint := "y/N"
	if m.value {
		hint = "Y/n"
	}
	if m.decided || m.aborted {
		answer := "No"
		if m.value {
			answer = "Yes"
		}
		return tea.NewView(fmt.Sprintf("%s %s %s\n", promptStyle.Render("?"), titleStyle.Render(m.prompt), answerStyle.Render(answer)))
	}
	return tea.NewView(fmt.Sprintf("%s %s %s ", promptStyle.Render("?"), titleStyle.Render(m.prompt), hintStyle.Render("["+hint+"]")))
}

// Confirm implements tui.Prompter.
func (p *Prompter) Confirm(ctx context.Context, prompt string, opts ...tui.ConfirmOption) (bool, error) {
	cfg := tui.ResolveConfirmConfig(opts)
	model := newConfirmModel(prompt, cfg)

	r := p.runProgram(ctx, model)
	if r.interrupted {
		return false, tui.ErrInterrupted
	}
	if r.err != nil {
		return false, fmt.Errorf("confirm prompt: %w", r.err)
	}

	m := r.model.(confirmModel)
	if m.aborted {
		return false, context.Canceled
	}
	return m.value, nil
}
