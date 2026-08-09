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
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// liveListUpdateMsg is the in-program form of LiveListUpdate; the pump
// goroutine in Prompter.LiveList forwards each external update as one.
type liveListUpdateMsg struct {
	Key   string
	Label string
	Err   error
}

// liveListModel embeds selectModel for cursor/filter/matched state and
// adds a Key→index map plus a per-row error set. Update, View, and
// Hints are overridden because Go method promotion isn't virtual —
// the embedded selectModel's View would otherwise call its own Hints
// against its own bindings.
type liveListModel struct {
	selectModel
	keyIndex map[string]int
	keys     []string // parallel to choices
	errs     map[int]bool
	// shadows selectModel.bindings; this slice drives dispatch + Hints
	bindings []KeyBinding[liveListModel]
}

// DefaultLiveListBindings returns a fresh copy of DefaultSelectBindings
// re-typed for *liveListModel.
func DefaultLiveListBindings() []KeyBinding[liveListModel] {
	return adaptSelectBindings(DefaultSelectBindings())
}

// adaptSelectBindings re-types each binding for *liveListModel by
// routing Label/Handle through the embedded selectModel.
func adaptSelectBindings(src []KeyBinding[selectModel]) []KeyBinding[liveListModel] {
	out := make([]KeyBinding[liveListModel], len(src))
	for i, b := range src {
		out[i] = KeyBinding[liveListModel]{
			ID:    b.ID,
			Match: b.Match,
			Label: func(m *liveListModel) string {
				if b.Label == nil {
					return ""
				}
				return b.Label(&m.selectModel)
			},
			Handle: func(m *liveListModel, msg tea.KeyPressMsg) (tea.Cmd, bool) {
				if b.Handle == nil {
					return nil, false
				}
				return b.Handle(&m.selectModel, msg)
			},
		}
	}
	return out
}

// WithLiveListAddBindings prepends extras so they outrank the defaults.
// See WithSelectAddBindings for semantics.
func WithLiveListAddBindings(extras ...KeyBinding[liveListModel]) tui.LiveListOption {
	return func(c *tui.LiveListConfig) {
		existing, _ := c.ExtraBindings.([]KeyBinding[liveListModel])
		c.ExtraBindings = append(existing, extras...)
	}
}

func newLiveListModel(prompt string, rows []tui.LiveRow, cfg tui.LiveListConfig) liveListModel {
	choices := make([]string, len(rows))
	keys := make([]string, len(rows))
	keyIndex := make(map[string]int, len(rows))
	for i, r := range rows {
		choices[i] = r.Label
		keys[i] = r.Key
		keyIndex[r.Key] = i
	}
	// Binding overrides are deliberately not passed to the embedded
	// selectModel — liveListModel.bindings is the active dispatch set.
	sm := newSelectModel(prompt, choices, tui.SelectConfig{
		Default:   cfg.Default,
		PageSize:  cfg.PageSize,
		Loop:      cfg.Loop,
		ShowHints: cfg.ShowHints,
		Hints:     cfg.Hints,
	})
	defaults := ApplyBindingOverrides(DefaultLiveListBindings(), cfg.RelabelByID, cfg.HiddenByID)
	var bindings []KeyBinding[liveListModel]
	if extras, ok := cfg.ExtraBindings.([]KeyBinding[liveListModel]); ok && len(extras) > 0 {
		bindings = make([]KeyBinding[liveListModel], 0, len(extras)+len(defaults))
		bindings = append(bindings, extras...)
		bindings = append(bindings, defaults...)
	} else {
		bindings = defaults
	}
	return liveListModel{
		selectModel: sm,
		keyIndex:    keyIndex,
		keys:        keys,
		errs:        make(map[int]bool),
		bindings:    bindings,
	}
}

func (m liveListModel) Init() tea.Cmd { return nil }

func (m liveListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case GoBackMsg:
		// Standalone mode quit; wizard composite intercepts before this.
		return m, tea.Quit
	case liveListUpdateMsg:
		if m.chosen {
			// Selection is locked in; bubbletea may still drain pumped
			// updates before teardown. Applying one here would refilter
			// and could empty matched, panicking View's chosen branch.
			return m, nil
		}
		idx, ok := m.keyIndex[v.Key]
		if !ok {
			return m, nil
		}
		m.choices[idx] = v.Label
		if v.Err != nil {
			m.errs[idx] = true
		} else {
			delete(m.errs, idx)
		}
		// Preserve cursor by Key across the refilter that follows: the
		// row at cursor may move when its label changes.
		cursorKey := ""
		if len(m.matched) > 0 && m.cursor >= 0 && m.cursor < len(m.matched) {
			cursorKey = m.keys[m.matched[m.cursor]]
		}
		m.refilter()
		if cursorKey != "" {
			for i, mi := range m.matched {
				if m.keys[mi] == cursorKey {
					m.cursor = i
					break
				}
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		cmd, _ := Dispatch(&m, m.bindings, v)
		return m, cmd
	}
	return m, nil
}

func (m liveListModel) View() tea.View {
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
		m.renderLiveHintBar(&b)
		return tea.NewView(b.String())
	}

	start, end := m.visibleRange()
	for i := start; i < end; i++ {
		choiceIdx := m.matched[i]
		label := m.choices[choiceIdx]
		errored := m.errs[choiceIdx]
		switch {
		case i == m.cursor && errored:
			fmt.Fprintf(&b, "  %s %s\n", cursorStyle.Render(">"), errorStyle.Render(label))
		case i == m.cursor:
			fmt.Fprintf(&b, "  %s %s\n", cursorStyle.Render(">"), selectedStyle.Render(label))
		case errored:
			fmt.Fprintf(&b, "    %s\n", errorStyle.Render(label))
		default:
			fmt.Fprintf(&b, "    %s\n", dimStyle.Render(label))
		}
	}
	m.renderLiveHintBar(&b)
	return tea.NewView(b.String())
}

// renderLiveHintBar mirrors selectModel.renderHintBar but routes
// through liveListModel.Hints so its own binding overrides apply.
func (m liveListModel) renderLiveHintBar(b *strings.Builder) {
	if !m.showHints {
		return
	}
	fmt.Fprintf(b, "\n%s\n", dimStyle.Render(strings.Join(m.Hints(), " · ")))
}

// Hints reads from liveListModel.bindings (overriding promoted
// selectModel.Hints). customHints (WithLiveListHints) wins.
func (m liveListModel) Hints() []string {
	if m.customHints != nil {
		return m.customHints
	}
	return HintsFor(&m, m.bindings)
}

// Result returns the selected row's original index (not the
// post-filter matched-set position).
func (m liveListModel) Result() (any, bool) {
	if !m.chosen || len(m.matched) == 0 {
		return nil, false
	}
	return m.matched[m.cursor], true
}

// NewLiveListPrompt builds a live-list model for the wizard composite;
// updates must be routed in by the composite as liveListUpdateMsg.
func NewLiveListPrompt(prompt string, rows []tui.LiveRow, cfg tui.LiveListConfig) PromptModel {
	return newLiveListModel(prompt, rows, cfg)
}

// pumpLiveListUpdates forwards updates to send. Exits on ctx done,
// done close (program returned), or updates close. Extracted so each
// branch is unit-testable without driving a real tea.Program.
func pumpLiveListUpdates(ctx context.Context, done <-chan struct{}, updates <-chan tui.LiveListUpdate, send func(liveListUpdateMsg)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case u, ok := <-updates:
			if !ok {
				return
			}
			send(liveListUpdateMsg{Key: u.Key, Label: u.Label, Err: u.Err})
		}
	}
}

// LiveList implements tui.LiveLister.
func (p *Prompter) LiveList(ctx context.Context, prompt string, rows []tui.LiveRow, updates <-chan tui.LiveListUpdate, opts ...tui.LiveListOption) (int, error) {
	if len(rows) == 0 {
		return -1, fmt.Errorf("live list prompt: no rows provided")
	}

	cfg := tui.ResolveLiveListConfig(opts)
	model := newLiveListModel(prompt, rows, cfg)

	prog := tea.NewProgram(model,
		tea.WithInput(p.in),
		tea.WithOutput(p.out),
		tea.WithContext(ctx),
	)

	// done is closed after prog.Run returns so the pump can't outlive
	// the program even when ctx stays alive and updates is never closed.
	done := make(chan struct{})
	if updates != nil {
		go pumpLiveListUpdates(ctx, done, updates, func(m liveListUpdateMsg) { prog.Send(m) })
	}

	result, err := prog.Run()
	close(done)
	interrupted := errors.Is(err, tea.ErrInterrupted)
	if !interrupted {
		if m, ok := result.(liveListModel); ok {
			interrupted = m.interrupted
		}
	}
	if interrupted {
		return -1, tui.ErrInterrupted
	}
	if err != nil {
		return -1, fmt.Errorf("live list prompt: %w", err)
	}

	m := result.(liveListModel)
	if m.aborted {
		return -1, context.Canceled
	}
	if !m.chosen || len(m.matched) == 0 {
		return -1, context.Canceled
	}
	return m.matched[m.cursor], nil
}
