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
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func TestSelectPrompt_Hints(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick one", []string{"a", "b"}, cfg)
	hints := m.Hints()
	if len(hints) == 0 {
		t.Fatal("expected hints")
	}
}

func TestSelectPrompt_ArrowAndEnter(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick", []string{"alpha", "beta"}, cfg)

	// Move down
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(PromptModel)

	// Press Enter
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PromptModel)

	val, done := m.Result()
	if !done {
		t.Fatal("expected done after Enter")
	}
	if val != 1 { // index 1 = "beta"
		t.Errorf("expected index 1, got %v", val)
	}
}

func TestSelectPrompt_EscWithFilter_ClearsFilter(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick", []string{"alpha", "beta"}, cfg)

	// Type to filter
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(PromptModel)

	// Esc should clear filter, not go back
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = updated.(PromptModel)

	// Should NOT produce GoBackMsg
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(GoBackMsg); ok {
			t.Fatal("Esc with active filter should clear filter, not go back")
		}
	}
}

func TestSelectPrompt_EscWithoutFilter_GoBack(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick", []string{"alpha", "beta"}, cfg)

	// Esc with no filter should produce GoBackMsg
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from Esc")
	}
	msg := cmd()
	if _, ok := msg.(GoBackMsg); !ok {
		t.Fatalf("expected GoBackMsg, got %T", msg)
	}
}

func TestSelectPrompt_CtrlC_NotHandled(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick", []string{"a", "b"}, cfg)

	// Ctrl+C should NOT be handled — composite intercepts it
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	pm := updated.(PromptModel)
	_, done := pm.Result()
	if done {
		t.Fatal("Ctrl+C should not complete the prompt")
	}
	// In non-wizard path, Ctrl+C sets interrupted and returns tea.Quit.
	// The composite intercepts Ctrl+C before the model sees it, so this
	// test verifies the model doesn't mark as "done" via Result().
	_ = cmd
}

func TestSelectPrompt_Result_NotDoneInitially(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := NewSelectPrompt("Pick", []string{"a"}, cfg)
	_, done := m.Result()
	if done {
		t.Fatal("should not be done initially")
	}
}

// TestSelectPrompt_WithShowHints_PlumbingE2E exercises the full option
// resolution path that callers use: SelectOption → ResolveSelectConfig →
// NewSelectPrompt → View. Catches breakage between the public API and the
// model internals.
func TestSelectPrompt_WithShowHints_PlumbingE2E(t *testing.T) {
	opts := []tui.SelectOption{tui.WithShowHints(true)}
	cfg := tui.ResolveSelectConfig(opts)

	if !cfg.ShowHints {
		t.Fatal("WithShowHints did not flow into config")
	}

	m := NewSelectPrompt("Pick", []string{"a", "b"}, cfg)
	view := m.View().Content
	if !strings.Contains(view, "↑/↓ navigate") {
		t.Errorf("hint bar missing from rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+c exit") {
		t.Errorf("default Hints() should include ctrl+c exit, got:\n%s", view)
	}
}

func TestSelectPrompt_WithHints_OverridePlumbingE2E(t *testing.T) {
	custom := []string{"↑/↓ move", "↵ open", "q quit"}
	opts := []tui.SelectOption{
		tui.WithShowHints(true),
		tui.WithHints(custom...),
	}
	cfg := tui.ResolveSelectConfig(opts)

	m := NewSelectPrompt("Pick", []string{"a", "b"}, cfg)

	if got := m.Hints(); !equalStrings(got, custom) {
		t.Errorf("Hints() = %v, want %v", got, custom)
	}
	view := m.View().Content
	if !strings.Contains(view, "↑/↓ move · ↵ open · q quit") {
		t.Errorf("override not rendered, got:\n%s", view)
	}
	if strings.Contains(view, "type to filter") {
		t.Errorf("default hint text should be replaced by override, got:\n%s", view)
	}
}

func TestSelectPrompt_WithHints_ZeroArgsFallsBackToDefaults(t *testing.T) {
	// WithHints() with no args produces a nil variadic slice — the model
	// should treat that as "use defaults," not "render an empty bar."
	opts := []tui.SelectOption{
		tui.WithShowHints(true),
		tui.WithHints(),
	}
	cfg := tui.ResolveSelectConfig(opts)
	m := NewSelectPrompt("Pick", []string{"a"}, cfg)

	hints := m.Hints()
	if len(hints) == 0 {
		t.Fatal("expected defaults, got empty")
	}
	if hints[0] != "↑/↓ navigate" {
		t.Errorf("expected default hints, got %v", hints)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
