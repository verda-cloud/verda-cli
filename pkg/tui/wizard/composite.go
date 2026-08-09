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

package wizard

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui/bubbletea"
)

const (
	// ActionNone indicates the prompt completed successfully (not a wizard action).
	ActionNone Action = -1
)

// promptResult carries the outcome of a prompt back to the engine.
type promptResult struct {
	value  any
	action Action // ActionExit, ActionBack, or ActionNone (success)
}

// showPromptMsg tells the composite to swap the active prompt.
type showPromptMsg struct {
	model   bubbletea.PromptModel
	stepMsg StepChangedMsg // broadcast to views
}

// compositeModel is the single tea.Model that runs for the entire wizard.
type compositeModel struct {
	bindings []KeyBinding
	bus      *MessageBus
	prompt   bubbletea.PromptModel
	resultCh chan promptResult
	hintBar  compositeHintBar
}

func newCompositeModel(bindings []KeyBinding, bus *MessageBus, resultCh chan promptResult) compositeModel {
	if bus == nil {
		bus = NewMessageBus()
	}
	var wizardHints []string
	for _, b := range bindings {
		if b.Label == "" {
			continue
		}
		wizardHints = append(wizardHints, b.Label)
	}
	return compositeModel{
		bindings: bindings,
		bus:      bus,
		resultCh: resultCh,
		hintBar:  compositeHintBar{wizardHints: wizardHints},
	}
}

func (m *compositeModel) setPrompt(p bubbletea.PromptModel) {
	m.prompt = p
	if p != nil {
		m.hintBar.promptHints = p.Hints()
	} else {
		m.hintBar.promptHints = nil
	}
}

func (m compositeModel) Init() tea.Cmd {
	if m.prompt != nil {
		return m.prompt.Init()
	}
	return nil
}

func (m compositeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Check wizard-level key bindings first.
		if action, ok := MatchBinding(m.bindings, msg); ok {
			m.resultCh <- promptResult{action: action}
			return m, nil
		}
		// Forward to active prompt.
		if m.prompt != nil {
			updated, cmd := m.prompt.Update(msg)
			m.prompt = updated.(bubbletea.PromptModel)
			// Re-read hints: prompt labels can be dynamic (e.g. esc flips
			// to "clear filter" once filtering), so a one-time snapshot at
			// setPrompt goes stale.
			m.hintBar.promptHints = m.prompt.Hints()
			// Check if prompt completed.
			if val, done := m.prompt.Result(); done {
				m.resultCh <- promptResult{value: val, action: ActionNone}
				return m, nil
			}
			return m, cmd
		}
		return m, nil

	case bubbletea.GoBackMsg:
		m.resultCh <- promptResult{action: ActionBack}
		return m, nil

	case showPromptMsg:
		m.setPrompt(msg.model)
		// Broadcast step change to views.
		m.bus.Broadcast(msg.stepMsg)
		// Initialize the new prompt.
		var cmd tea.Cmd
		if m.prompt != nil {
			cmd = m.prompt.Init()
		}
		return m, cmd

	default:
		// Forward non-key messages to prompt (e.g. blink, window size).
		if m.prompt != nil {
			updated, cmd := m.prompt.Update(msg)
			m.prompt = updated.(bubbletea.PromptModel)
			return m, cmd
		}
	}
	return m, nil
}

func (m compositeModel) View() tea.View {
	var sections []string

	// Render views from the message bus.
	for _, output := range m.bus.RenderAll() {
		if output != "" {
			sections = append(sections, output)
		}
	}

	// Active prompt.
	if m.prompt != nil {
		sections = append(sections, m.prompt.View().Content)
	}

	// Hint bar.
	if hint := m.hintBar.render(); hint != "" {
		sections = append(sections, hint)
	}

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// compositeHintBar merges wizard-level and prompt-level hints.
type compositeHintBar struct {
	wizardHints []string
	promptHints []string
}

func (h *compositeHintBar) render() string {
	all := make([]string, 0, len(h.promptHints)+len(h.wizardHints))
	all = append(all, h.promptHints...)
	all = append(all, h.wizardHints...)
	if len(all) == 0 {
		return ""
	}
	return "  " + strings.Join(all, " · ")
}
