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

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

type textInputModel struct {
	prompt      string
	textInput   textinput.Model
	submitted   bool
	aborted     bool
	interrupted bool // true for Ctrl+C (hard cancel), false for Esc (soft cancel)
	pristine    bool // true until user types first character; typing clears the pre-filled default
	validate    func(string) error
	err         error
	bindings    []KeyBinding[textInputModel]
}

// DefaultTextInputBindings returns a fresh copy of the canonical
// binding set. Stable IDs: submit, esc, exit. An unlisted
// pristine-clear binding runs once on the first printable keystroke.
func DefaultTextInputBindings() []KeyBinding[textInputModel] {
	return []KeyBinding[textInputModel]{
		{
			ID:    "submit",
			Match: MatchKey(tea.KeyEnter),
			Label: func(*textInputModel) string { return hintEnterEntry },
			Handle: func(m *textInputModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.validate != nil {
					if err := m.validate(m.textInput.Value()); err != nil {
						m.err = err
						return nil, true
					}
				}
				m.submitted = true
				return tea.Quit, true
			},
		},
		{
			ID:    "esc",
			Match: MatchKey(tea.KeyEscape),
			Label: func(*textInputModel) string { return hintEscBack },
			Handle: func(m *textInputModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.aborted = true
				return func() tea.Msg { return GoBackMsg{} }, true
			},
		},
		{
			ID:    "exit",
			Match: MatchRune('c', tea.ModCtrl),
			Label: func(*textInputModel) string { return hintCtrlCExit },
			Handle: func(m *textInputModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.interrupted = true
				return tea.Quit, true
			},
		},
		{
			// Clears the pre-filled default on the first printable
			// keystroke so the user's input replaces it instead of
			// appending. Always returns stop=false: textInput.Update
			// must still see the key to insert it.
			ID:    "pristine-clear",
			Match: func(_ tea.KeyPressMsg) bool { return true },
			Label: func(*textInputModel) string { return "" },
			Handle: func(m *textInputModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.pristine {
					m.pristine = false
					if msg.Text != "" && msg.Mod == 0 {
						m.textInput.SetValue("")
					}
				}
				return nil, false
			},
		},
	}
}

// WithTextInputAddBindings prepends extras so they outrank the defaults.
// See WithSelectAddBindings for semantics.
func WithTextInputAddBindings(extras ...KeyBinding[textInputModel]) tui.TextInputOption {
	return func(c *tui.TextInputConfig) {
		existing, _ := c.ExtraBindings.([]KeyBinding[textInputModel])
		c.ExtraBindings = append(existing, extras...)
	}
}

func newTextInputModel(prompt string, cfg tui.TextInputConfig) textInputModel {
	ti := textinput.New()
	ti.Placeholder = cfg.Placeholder
	ti.SetValue(cfg.Default)
	ti.Focus()

	defaults := ApplyBindingOverrides(DefaultTextInputBindings(), cfg.RelabelByID, cfg.HiddenByID)
	var bindings []KeyBinding[textInputModel]
	if extras, ok := cfg.ExtraBindings.([]KeyBinding[textInputModel]); ok && len(extras) > 0 {
		bindings = make([]KeyBinding[textInputModel], 0, len(extras)+len(defaults))
		bindings = append(bindings, extras...)
		bindings = append(bindings, defaults...)
	} else {
		bindings = defaults
	}

	return textInputModel{
		prompt:    prompt,
		textInput: ti,
		pristine:  cfg.Default != "",
		validate:  cfg.Validate,
		bindings:  bindings,
	}
}

func (m textInputModel) Init() tea.Cmd { return textinput.Blink }

func (m textInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(GoBackMsg); ok {
		return m, tea.Quit // standalone mode quit; wizard composite intercepts before this
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if cmd, stopped := Dispatch(&m, m.bindings, key); stopped {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	m.err = nil
	return m, cmd
}

// Hints derives key hints from the resolved bindings.
func (m textInputModel) Hints() []string {
	return HintsFor(&m, m.bindings)
}

// Result returns the text input value after the user submits.
func (m textInputModel) Result() (any, bool) {
	return m.textInput.Value(), m.submitted
}

// NewTextInputPrompt creates a text input prompt model for use in the wizard composite.
func NewTextInputPrompt(prompt string, cfg tui.TextInputConfig) PromptModel {
	return newTextInputModel(prompt, cfg)
}

func (m textInputModel) View() tea.View {
	if m.submitted {
		return tea.NewView(fmt.Sprintf("%s %s %s\n", promptStyle.Render("?"), titleStyle.Render(m.prompt), answerStyle.Render(m.textInput.Value())))
	}
	s := fmt.Sprintf("%s %s\n%s", promptStyle.Render("?"), titleStyle.Render(m.prompt), m.textInput.View())
	if m.err != nil {
		s += fmt.Sprintf("\n  %s", errorStyle.Render("✗ "+m.err.Error()))
	}
	return tea.NewView(s)
}

// TextInput implements tui.Prompter.
func (p *Prompter) TextInput(ctx context.Context, prompt string, opts ...tui.TextInputOption) (string, error) {
	cfg := tui.ResolveTextInputConfig(opts)
	model := newTextInputModel(prompt, cfg)

	r := p.runProgram(ctx, model)
	if r.interrupted {
		return "", tui.ErrInterrupted
	}
	if r.err != nil {
		return "", fmt.Errorf("text input prompt: %w", r.err)
	}

	m := r.model.(textInputModel)
	if m.aborted {
		return "", context.Canceled
	}
	return m.textInput.Value(), nil
}
