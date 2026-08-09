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
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

type selectModel struct {
	prompt      string
	choices     []string // original full list — never mutated
	filter      string   // current filter text
	matched     []int    // indices into choices that match filter
	cursor      int      // position within matched (not choices)
	pageSize    int
	loop        bool
	showHints   bool
	customHints []string                  // nil = derive from bindings
	bindings    []KeyBinding[selectModel] // resolved (defaults + overrides + extras)
	chosen      bool
	aborted     bool
	interrupted bool // true for Ctrl+C (hard cancel), false for Esc (soft cancel)
}

// DefaultSelectBindings returns a fresh copy of the canonical binding
// set. Stable IDs for WithSelectRelabel / WithSelectHide: navigate,
// vim-up, vim-down, filter-type, select, esc, filter-backspace, exit.
func DefaultSelectBindings() []KeyBinding[selectModel] {
	return []KeyBinding[selectModel]{
		{
			ID:    "navigate",
			Match: MatchKey(tea.KeyUp, tea.KeyDown),
			Label: func(*selectModel) string { return "↑/↓ navigate" },
			Handle: func(m *selectModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				if msg.Code == tea.KeyUp {
					m.moveUp()
				} else {
					m.moveDown()
				}
				return nil, true
			},
		},
		{
			ID:    "vim-up",
			Match: MatchRune('k'),
			Label: func(*selectModel) string { return "" },
			// j/k navigate when filter is empty; otherwise pass through so
			// filter-type appends the key to the filter.
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.filter != "" {
					return nil, false
				}
				m.moveUp()
				return nil, true
			},
		},
		{
			ID:    "vim-down",
			Match: MatchRune('j'),
			Label: func(*selectModel) string { return "" },
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.filter != "" {
					return nil, false
				}
				m.moveDown()
				return nil, true
			},
		},
		{
			ID:    "filter-type",
			Match: MatchText(),
			Label: func(*selectModel) string { return "type to filter" },
			Handle: func(m *selectModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				m.filter += msg.Text
				m.refilter()
				return nil, true
			},
		},
		{
			ID:    "select",
			Match: MatchKey(tea.KeyEnter),
			Label: func(*selectModel) string { return "enter select" },
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if len(m.matched) == 0 {
					return nil, true
				}
				m.chosen = true
				return tea.Quit, true
			},
		},
		{
			ID:    "esc",
			Match: MatchKey(tea.KeyEscape),
			Label: func(m *selectModel) string {
				if m.filter != "" {
					return "esc clear filter"
				}
				return hintEscBack
			},
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.filter != "" {
					m.filter = ""
					m.refilter()
					return nil, true
				}
				m.aborted = true
				return func() tea.Msg { return GoBackMsg{} }, true
			},
		},
		{
			ID:    "filter-backspace",
			Match: MatchKey(tea.KeyBackspace),
			Label: func(*selectModel) string { return "" },
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if len(m.filter) > 0 {
					_, size := utf8.DecodeLastRuneInString(m.filter)
					m.filter = m.filter[:len(m.filter)-size]
					m.refilter()
				}
				return nil, true
			},
		},
		{
			ID:    "exit",
			Match: MatchRune('c', tea.ModCtrl),
			Label: func(*selectModel) string { return hintCtrlCExit },
			Handle: func(m *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.interrupted = true
				return tea.Quit, true
			},
		},
	}
}

// WithSelectAddBindings prepends extras so they outrank the default
// catch-all matchers (e.g. a '?' help binding won't be swallowed by
// filter-type). KeyBinding[selectModel] references an unexported type,
// so only in-package callers can construct one — external code uses
// WithSelectRelabel / WithSelectHide for label-only changes.
func WithSelectAddBindings(extras ...KeyBinding[selectModel]) tui.SelectOption {
	return func(c *tui.SelectConfig) {
		existing, _ := c.ExtraBindings.([]KeyBinding[selectModel])
		c.ExtraBindings = append(existing, extras...)
	}
}

func newSelectModel(prompt string, choices []string, cfg tui.SelectConfig) selectModel {
	ps := cfg.PageSize
	if ps <= 0 || ps > len(choices) {
		ps = len(choices)
	}
	cursor := cfg.Default
	if cursor < 0 || cursor >= len(choices) {
		cursor = 0
	}
	matched := make([]int, len(choices))
	for i := range choices {
		matched[i] = i
	}
	defaults := ApplyBindingOverrides(DefaultSelectBindings(), cfg.RelabelByID, cfg.HiddenByID)
	var bindings []KeyBinding[selectModel]
	if extras, ok := cfg.ExtraBindings.([]KeyBinding[selectModel]); ok && len(extras) > 0 {
		bindings = make([]KeyBinding[selectModel], 0, len(extras)+len(defaults))
		bindings = append(bindings, extras...)
		bindings = append(bindings, defaults...)
	} else {
		bindings = defaults
	}
	return selectModel{
		prompt:      prompt,
		choices:     choices,
		filter:      "",
		matched:     matched,
		cursor:      cursor,
		pageSize:    ps,
		loop:        cfg.Loop,
		showHints:   cfg.ShowHints,
		customHints: cfg.Hints,
		bindings:    bindings,
	}
}

func (m *selectModel) refilter() {
	m.matched = refilter(m.filter, m.choices, m.matched)
	m.cursor = 0
}

func (m *selectModel) moveUp() {
	if len(m.matched) == 0 {
		return
	}
	if m.cursor > 0 {
		m.cursor--
	} else if m.loop {
		m.cursor = len(m.matched) - 1
	}
}

func (m *selectModel) moveDown() {
	if len(m.matched) == 0 {
		return
	}
	if m.cursor < len(m.matched)-1 {
		m.cursor++
	} else if m.loop {
		m.cursor = 0
	}
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(GoBackMsg); ok {
		// Standalone mode quit: wizard composite intercepts GoBackMsg
		// before forwarding, so this branch fires only outside a wizard.
		return m, tea.Quit
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	cmd, _ := Dispatch(&m, m.bindings, key)
	return m, cmd
}

func (m selectModel) View() tea.View {
	if m.chosen {
		selected := m.choices[m.matched[m.cursor]]
		return tea.NewView(fmt.Sprintf("%s %s %s\n", promptStyle.Render("?"), titleStyle.Render(m.prompt), answerStyle.Render(selected)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", promptStyle.Render("?"), titleStyle.Render(m.prompt))
	if m.filter != "" {
		fmt.Fprintf(&b, " %s", answerStyle.Render(m.filter))
	}
	b.WriteString("\n")

	if len(m.matched) == 0 {
		fmt.Fprintf(&b, "    %s\n", dimStyle.Render("no matches"))
		m.renderHintBar(&b)
		return tea.NewView(b.String())
	}

	start, end := m.visibleRange()
	for i := start; i < end; i++ {
		label := m.choices[m.matched[i]]
		if i == m.cursor {
			fmt.Fprintf(&b, "  %s %s\n", cursorStyle.Render(">"), selectedStyle.Render(label))
		} else {
			fmt.Fprintf(&b, "    %s\n", dimStyle.Render(label))
		}
	}
	m.renderHintBar(&b)
	return tea.NewView(b.String())
}

func (m selectModel) renderHintBar(b *strings.Builder) {
	if !m.showHints {
		return
	}
	fmt.Fprintf(b, "\n%s\n", dimStyle.Render(strings.Join(m.Hints(), " · ")))
}

func (m selectModel) visibleRange() (int, int) {
	return visibleWindow(len(m.matched), m.cursor, m.pageSize)
}

// Hints returns the hint bar entries. customHints (from WithHints)
// wins; otherwise hints derive from the binding set via HintsFor.
func (m selectModel) Hints() []string {
	if m.customHints != nil {
		return m.customHints
	}
	return HintsFor(&m, m.bindings)
}

// Result returns the selected index after the user presses Enter.
func (m selectModel) Result() (any, bool) {
	if !m.chosen || len(m.matched) == 0 {
		return nil, false
	}
	return m.matched[m.cursor], true
}

// NewSelectPrompt creates a select prompt model for use in the wizard composite.
// The returned model handles domain keys only (arrows, Enter, type-to-filter, Esc).
// Ctrl+C is NOT handled — the composite model intercepts it.
func NewSelectPrompt(prompt string, choices []string, cfg tui.SelectConfig) PromptModel {
	return newSelectModel(prompt, choices, cfg)
}

// Select implements tui.Prompter.
func (p *Prompter) Select(ctx context.Context, prompt string, choices []string, opts ...tui.SelectOption) (int, error) {
	if len(choices) == 0 {
		return -1, fmt.Errorf("select prompt: no choices provided")
	}

	cfg := tui.ResolveSelectConfig(opts)
	model := newSelectModel(prompt, choices, cfg)

	r := p.runProgram(ctx, model)
	if r.interrupted {
		return -1, tui.ErrInterrupted
	}
	if r.err != nil {
		return -1, fmt.Errorf("select prompt: %w", r.err)
	}

	m := r.model.(selectModel)
	if m.aborted {
		return -1, context.Canceled
	}
	return m.matched[m.cursor], nil
}
