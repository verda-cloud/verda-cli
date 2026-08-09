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
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

type editorModel struct {
	prompt    string
	textarea  textarea.Model
	hint      string                 // resolved: caller override or library default
	showHint  bool                   // false = suppress the affordance line (WithEditorNoHint)
	summary   func(lines int) string // resolved: caller override or library default
	submitted bool
	aborted   bool
}

func newEditorModel(prompt string, cfg tui.EditorConfig) editorModel {
	ta := textarea.New()
	ta.SetValue(cfg.Default)
	ta.ShowLineNumbers = true
	ta.Focus()

	hint := cfg.Hint
	if hint == "" {
		hint = "ctrl+d to submit, esc to cancel"
	}
	summary := cfg.Summary
	if summary == nil {
		summary = func(lines int) string { return fmt.Sprintf("[%d lines]", lines) }
	}

	return editorModel{
		prompt:   prompt,
		textarea: ta,
		hint:     hint,
		showHint: !cfg.NoHint,
		summary:  summary,
	}
}

func (m editorModel) Init() tea.Cmd { return textarea.Blink }

func (m editorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+d":
			m.submitted = true
			return m, tea.Quit
		case keyCtrlC, keyEsc:
			m.aborted = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m editorModel) View() tea.View {
	if m.submitted {
		lines := strings.Count(m.textarea.Value(), "\n") + 1
		return tea.NewView(fmt.Sprintf("? %s %s\n", m.prompt, m.summary(lines)))
	}
	if !m.showHint {
		return tea.NewView(fmt.Sprintf("? %s\n%s", m.prompt, m.textarea.View()))
	}
	return tea.NewView(fmt.Sprintf("? %s (%s)\n%s", m.prompt, m.hint, m.textarea.View()))
}

// Editor implements tui.Prompter.
func (p *Prompter) Editor(ctx context.Context, prompt string, opts ...tui.EditorOption) (string, error) {
	cfg := tui.ResolveEditorConfig(opts)
	model := newEditorModel(prompt, cfg)

	program := tea.NewProgram(model,
		tea.WithInput(p.in),
		tea.WithOutput(p.out),
		tea.WithContext(ctx),
	)

	result, err := program.Run()
	if err != nil {
		return "", fmt.Errorf("editor prompt: %w", err)
	}

	m := result.(editorModel)
	if m.aborted {
		return "", context.Canceled
	}
	return m.textarea.Value(), nil
}
