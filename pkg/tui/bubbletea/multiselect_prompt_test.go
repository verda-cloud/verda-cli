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

func TestMultiSelectPrompt_Hints(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig(nil)
	m := NewMultiSelectPrompt("Pick", []string{"a", "b"}, cfg)
	hints := m.Hints()
	if len(hints) == 0 {
		t.Fatal("expected hints")
	}
}

func TestMultiSelectPrompt_SpaceAndEnter(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig(nil)
	m := NewMultiSelectPrompt("Pick", []string{"alpha", "beta", "gamma"}, cfg)

	// Space to toggle first item
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(PromptModel)

	// Down + Space to toggle second
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(PromptModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(PromptModel)

	// Enter to confirm
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PromptModel)

	val, done := m.Result()
	if !done {
		t.Fatal("expected done after Enter")
	}
	indices, ok := val.([]int)
	if !ok {
		t.Fatalf("expected []int, got %T", val)
	}
	if len(indices) != 2 || indices[0] != 0 || indices[1] != 1 {
		t.Errorf("expected [0 1], got %v", indices)
	}
}

func TestMultiSelectPrompt_EscGoBack(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig(nil)
	m := NewMultiSelectPrompt("Pick", []string{"a"}, cfg)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from Esc")
	}
	msg := cmd()
	if _, ok := msg.(GoBackMsg); !ok {
		t.Fatalf("expected GoBackMsg, got %T", msg)
	}
}

func TestMultiSelectPrompt_Result_NotDoneInitially(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig(nil)
	m := NewMultiSelectPrompt("Pick", []string{"a"}, cfg)
	_, done := m.Result()
	if done {
		t.Fatal("should not be done initially")
	}
}

func TestMultiSelectPrompt_WithShowHints_PlumbingE2E(t *testing.T) {
	opts := []tui.MultiSelectOption{tui.WithMultiSelectShowHints(true)}
	cfg := tui.ResolveMultiSelectConfig(opts)
	if !cfg.ShowHints {
		t.Fatal("WithMultiSelectShowHints did not flow into config")
	}

	m := NewMultiSelectPrompt("Pick", []string{"a", "b"}, cfg)
	view := m.View().Content
	if !strings.Contains(view, "↑/↓ navigate") {
		t.Errorf("hint bar missing, got:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+c exit") {
		t.Errorf("default Hints() should include ctrl+c exit, got:\n%s", view)
	}
}

func TestMultiSelectPrompt_WithMultiSelectHints_OverridePlumbingE2E(t *testing.T) {
	custom := []string{"↑/↓ move", "␣ check", "↵ done"}
	opts := []tui.MultiSelectOption{
		tui.WithMultiSelectShowHints(true),
		tui.WithMultiSelectHints(custom...),
	}
	cfg := tui.ResolveMultiSelectConfig(opts)

	m := NewMultiSelectPrompt("Pick", []string{"a", "b"}, cfg)
	hints := m.Hints()
	if len(hints) != len(custom) {
		t.Fatalf("Hints() length = %d, want %d", len(hints), len(custom))
	}
	for i := range custom {
		if hints[i] != custom[i] {
			t.Errorf("Hints()[%d] = %q, want %q", i, hints[i], custom[i])
		}
	}
	view := m.View().Content
	if !strings.Contains(view, "↑/↓ move · ␣ check · ↵ done") {
		t.Errorf("override not rendered, got:\n%s", view)
	}
	if strings.Contains(view, "ctrl+a select all") {
		t.Errorf("default hint text should be replaced by override, got:\n%s", view)
	}
}

func TestMultiSelectPrompt_WithMultiSelectHints_ZeroArgsFallsBackToDefaults(t *testing.T) {
	opts := []tui.MultiSelectOption{
		tui.WithMultiSelectShowHints(true),
		tui.WithMultiSelectHints(),
	}
	cfg := tui.ResolveMultiSelectConfig(opts)
	m := NewMultiSelectPrompt("Pick", []string{"a"}, cfg)
	hints := m.Hints()
	if len(hints) == 0 || hints[0] != "↑/↓ navigate" {
		t.Errorf("expected defaults, got %v", hints)
	}
}
