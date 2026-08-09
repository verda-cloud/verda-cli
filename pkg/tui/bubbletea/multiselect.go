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

type multiSelectModel struct {
	prompt      string
	choices     []string
	filter      string
	matched     []int // indices into choices that match filter
	cursor      int   // position within matched (not choices)
	selected    map[int]bool
	pageSize    int
	loop        bool
	min         int
	max         int
	minErr      func(min int) string // resolved: caller override or library default
	maxErr      func(max int) string // resolved: caller override or library default
	showHints   bool
	customHints []string                       // nil = derive from bindings
	bindings    []KeyBinding[multiSelectModel] // resolved (defaults + overrides + extras)
	done        bool
	aborted     bool
	interrupted bool // true for Ctrl+C (hard cancel), false for Esc (soft cancel)
	err         string
}

// DefaultMultiSelectBindings returns a fresh copy of the canonical
// binding set. Stable IDs for WithMultiSelectRelabel / Hide: navigate,
// vim-up, vim-down, toggle, select-all, filter-type, confirm, esc,
// filter-backspace, exit.
func DefaultMultiSelectBindings() []KeyBinding[multiSelectModel] {
	return []KeyBinding[multiSelectModel]{
		{
			ID:    "navigate",
			Match: MatchKey(tea.KeyUp, tea.KeyDown),
			Label: func(*multiSelectModel) string { return "↑/↓ navigate" },
			Handle: func(m *multiSelectModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
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
			Label: func(*multiSelectModel) string { return "" },
			// j/k navigate when filter is empty; otherwise pass through
			// so filter-type appends the key to the filter.
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
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
			Label: func(*multiSelectModel) string { return "" },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.filter != "" {
					return nil, false
				}
				m.moveDown()
				return nil, true
			},
		},
		{
			ID:    "toggle",
			Match: MatchKey(tea.KeySpace),
			Label: func(*multiSelectModel) string { return "space toggle" },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if len(m.matched) == 0 {
					return nil, true
				}
				idx := m.matched[m.cursor]
				if m.selected[idx] {
					delete(m.selected, idx)
					m.err = ""
				} else {
					if m.max > 0 && len(m.selected) >= m.max {
						m.err = m.maxErr(m.max)
					} else {
						m.selected[idx] = true
						m.err = ""
					}
				}
				return nil, true
			},
		},
		{
			ID:    "select-all",
			Match: MatchRune('a', tea.ModCtrl),
			Label: func(*multiSelectModel) string { return "ctrl+a select all" },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.toggleAll()
				return nil, true
			},
		},
		{
			ID:    "filter-type",
			Match: MatchText(),
			Label: func(*multiSelectModel) string { return "type to filter" },
			Handle: func(m *multiSelectModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				m.filter += msg.Text
				m.refilter()
				return nil, true
			},
		},
		{
			ID:    "confirm",
			Match: MatchKey(tea.KeyEnter),
			Label: func(*multiSelectModel) string { return "enter confirm" },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				if m.min > 0 && len(m.selected) < m.min {
					m.err = m.minErr(m.min)
					return nil, true
				}
				m.done = true
				return tea.Quit, true
			},
		},
		{
			ID:    "esc",
			Match: MatchKey(tea.KeyEscape),
			Label: func(m *multiSelectModel) string {
				if m.filter != "" {
					return "esc clear filter"
				}
				return hintEscBack
			},
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
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
			Label: func(*multiSelectModel) string { return "" },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
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
			Label: func(*multiSelectModel) string { return hintCtrlCExit },
			Handle: func(m *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
				m.interrupted = true
				return tea.Quit, true
			},
		},
	}
}

// WithMultiSelectAddBindings prepends extras so they outrank the default
// catch-all matchers. See WithSelectAddBindings for full semantics.
func WithMultiSelectAddBindings(extras ...KeyBinding[multiSelectModel]) tui.MultiSelectOption {
	return func(c *tui.MultiSelectConfig) {
		existing, _ := c.ExtraBindings.([]KeyBinding[multiSelectModel])
		c.ExtraBindings = append(existing, extras...)
	}
}

func newMultiSelectModel(prompt string, choices []string, cfg tui.MultiSelectConfig) multiSelectModel {
	ps := cfg.PageSize
	if ps <= 0 || ps > len(choices) {
		ps = len(choices)
	}
	selected := make(map[int]bool)
	for _, idx := range cfg.Defaults {
		if idx >= 0 && idx < len(choices) {
			selected[idx] = true
		}
	}
	matched := make([]int, len(choices))
	for i := range choices {
		matched[i] = i
	}
	minErr := cfg.MinError
	if minErr == nil {
		minErr = func(min int) string {
			return fmt.Sprintf("at least %d selections required — press space to select", min)
		}
	}
	maxErr := cfg.MaxError
	if maxErr == nil {
		maxErr = func(max int) string {
			return fmt.Sprintf("maximum %d selections allowed", max)
		}
	}
	defaults := ApplyBindingOverrides(DefaultMultiSelectBindings(), cfg.RelabelByID, cfg.HiddenByID)
	var bindings []KeyBinding[multiSelectModel]
	if extras, ok := cfg.ExtraBindings.([]KeyBinding[multiSelectModel]); ok && len(extras) > 0 {
		bindings = make([]KeyBinding[multiSelectModel], 0, len(extras)+len(defaults))
		bindings = append(bindings, extras...)
		bindings = append(bindings, defaults...)
	} else {
		bindings = defaults
	}
	return multiSelectModel{
		prompt:      prompt,
		choices:     choices,
		matched:     matched,
		selected:    selected,
		pageSize:    ps,
		loop:        cfg.Loop,
		min:         cfg.Min,
		max:         cfg.Max,
		minErr:      minErr,
		maxErr:      maxErr,
		showHints:   cfg.ShowHints,
		customHints: cfg.Hints,
		bindings:    bindings,
	}
}

func (m *multiSelectModel) refilter() {
	m.matched = refilter(m.filter, m.choices, m.matched)
	m.cursor = 0
}

func (m *multiSelectModel) moveUp() {
	if len(m.matched) == 0 {
		return
	}
	if m.cursor > 0 {
		m.cursor--
	} else if m.loop {
		m.cursor = len(m.matched) - 1
	}
}

func (m *multiSelectModel) moveDown() {
	if len(m.matched) == 0 {
		return
	}
	if m.cursor < len(m.matched)-1 {
		m.cursor++
	} else if m.loop {
		m.cursor = 0
	}
}

// toggleAll selects or deselects all matched (visible) items.
func (m *multiSelectModel) toggleAll() {
	allSelected := true
	for _, idx := range m.matched {
		if !m.selected[idx] {
			allSelected = false
			break
		}
	}
	if allSelected {
		for _, idx := range m.matched {
			delete(m.selected, idx)
		}
	} else {
		for _, idx := range m.matched {
			if m.max > 0 && len(m.selected) >= m.max {
				m.err = m.maxErr(m.max)
				return
			}
			m.selected[idx] = true
		}
	}
	m.err = ""
}

func (m multiSelectModel) Init() tea.Cmd { return nil }

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

// Hints returns the hint bar entries. customHints (from WithMultiSelectHints)
// wins; otherwise hints derive from the binding set via HintsFor.
func (m multiSelectModel) Hints() []string {
	if m.customHints != nil {
		return m.customHints
	}
	return HintsFor(&m, m.bindings)
}

// Result returns the selected indices after the user confirms.
func (m multiSelectModel) Result() (any, bool) {
	if !m.done {
		return nil, false
	}
	var indices []int
	for i := range m.choices {
		if m.selected[i] {
			indices = append(indices, i)
		}
	}
	return indices, true
}

// NewMultiSelectPrompt creates a multiselect prompt model for use in the wizard composite.
func NewMultiSelectPrompt(prompt string, choices []string, cfg tui.MultiSelectConfig) PromptModel {
	return newMultiSelectModel(prompt, choices, cfg)
}

func (m multiSelectModel) View() tea.View {
	if m.done {
		var names []string
		for i, c := range m.choices {
			if m.selected[i] {
				names = append(names, c)
			}
		}
		return tea.NewView(fmt.Sprintf("%s %s %s\n", promptStyle.Render("?"), titleStyle.Render(m.prompt), answerStyle.Render(strings.Join(names, ", "))))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", promptStyle.Render("?"), titleStyle.Render(m.prompt))
	if m.filter != "" {
		fmt.Fprintf(&b, " %s", answerStyle.Render(m.filter))
	}
	b.WriteString("\n")

	if len(m.matched) == 0 {
		fmt.Fprintf(&b, "    %s\n", dimStyle.Render("no matches"))
	} else {
		start, end := m.visibleRange()
		for i := start; i < end; i++ {
			idx := m.matched[i]
			cur := "  "
			if i == m.cursor {
				cur = cursorStyle.Render("> ")
			}
			check := uncheckStyle.Render("[ ]")
			label := dimStyle.Render(m.choices[idx])
			if m.selected[idx] {
				check = checkStyle.Render("[x]")
				label = selectedStyle.Render(m.choices[idx])
			}
			if i == m.cursor && !m.selected[idx] {
				label = selectedStyle.Render(m.choices[idx])
			}
			fmt.Fprintf(&b, "  %s%s %s\n", cur, check, label)
		}
	}

	if m.err != "" {
		fmt.Fprintf(&b, "  %s\n", errorStyle.Render("✗ "+m.err))
	}
	if m.showHints {
		fmt.Fprintf(&b, "\n%s\n", dimStyle.Render(strings.Join(m.Hints(), " · ")))
	}
	return tea.NewView(b.String())
}

func (m multiSelectModel) visibleRange() (int, int) {
	return visibleWindow(len(m.matched), m.cursor, m.pageSize)
}

// MultiSelect implements tui.Prompter.
func (p *Prompter) MultiSelect(ctx context.Context, prompt string, choices []string, opts ...tui.MultiSelectOption) ([]int, error) {
	if len(choices) == 0 {
		return nil, fmt.Errorf("multi-select prompt: no choices provided")
	}

	cfg := tui.ResolveMultiSelectConfig(opts)
	model := newMultiSelectModel(prompt, choices, cfg)

	r := p.runProgram(ctx, model)
	if r.interrupted {
		return nil, tui.ErrInterrupted
	}
	if r.err != nil {
		return nil, fmt.Errorf("multi-select prompt: %w", r.err)
	}

	m := r.model.(multiSelectModel)
	if m.aborted {
		return nil, context.Canceled
	}

	var indices []int
	for i := range m.choices {
		if m.selected[i] {
			indices = append(indices, i)
		}
	}
	return indices, nil
}
