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

func TestMultiSelectBindings_DynamicEscLabel(t *testing.T) {
	m := newMS([]string{"alpha", "beta"}, func(c *tui.MultiSelectConfig) { c.ShowHints = true })

	if !containsString(m.Hints(), "esc back") {
		t.Errorf("expected 'esc back' before filtering, got %v", m.Hints())
	}
	m = msRune(m, 'a')
	if !containsString(m.Hints(), "esc clear filter") {
		t.Errorf("expected 'esc clear filter' while filtering, got %v", m.Hints())
	}
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !containsString(m.Hints(), "esc back") {
		t.Errorf("expected 'esc back' after filter cleared, got %v", m.Hints())
	}
}

func TestMultiSelectBindings_WithMultiSelectRelabel(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig([]tui.MultiSelectOption{
		tui.WithMultiSelectShowHints(true),
		tui.WithMultiSelectRelabel("toggle", "␣ pick"),
		tui.WithMultiSelectRelabel("confirm", "↵ done"),
	})
	m := newMultiSelectModel("Pick", []string{"a"}, cfg)

	hints := m.Hints()
	if !containsString(hints, "␣ pick") || !containsString(hints, "↵ done") {
		t.Errorf("expected relabels, got %v", hints)
	}
	if containsString(hints, "space toggle") || containsString(hints, "enter confirm") {
		t.Errorf("relabels did not replace defaults, got %v", hints)
	}
}

func TestMultiSelectBindings_WithMultiSelectHide(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig([]tui.MultiSelectOption{
		tui.WithMultiSelectShowHints(true),
		tui.WithMultiSelectHide("exit", "select-all"),
	})
	m := newMultiSelectModel("Pick", []string{"a"}, cfg)

	hints := m.Hints()
	if containsString(hints, "ctrl+c exit") || containsString(hints, "ctrl+a select all") {
		t.Errorf("hidden entries leaked into hints, got %v", hints)
	}

	// Ctrl+A still toggles all (key handling preserved).
	m = msCtrl(m, 'a')
	if !m.selected[0] {
		t.Error("ctrl+a should still select-all even when label hidden")
	}
}

func TestMultiSelectBindings_WithMultiSelectAddBindings(t *testing.T) {
	helped := false
	help := KeyBinding[multiSelectModel]{
		ID:    "help",
		Match: MatchRune('?'),
		Label: func(*multiSelectModel) string { return "? help" },
		Handle: func(_ *multiSelectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			helped = true
			return nil, true
		},
	}
	cfg := tui.ResolveMultiSelectConfig([]tui.MultiSelectOption{
		tui.WithMultiSelectShowHints(true),
		WithMultiSelectAddBindings(help),
	})
	m := newMultiSelectModel("Pick", []string{"a"}, cfg)

	if !containsString(m.Hints(), "? help") {
		t.Errorf("custom binding label missing, got %v", m.Hints())
	}
	m = msRune(m, '?')
	if !helped {
		t.Error("custom binding handler did not fire")
	}
	if m.filter == "?" {
		t.Error("custom binding should have stopped before filter-type")
	}
}

func TestMultiSelectBindings_DefaultHintsOrder(t *testing.T) {
	cfg := tui.ResolveMultiSelectConfig(nil)
	m := newMultiSelectModel("Pick", []string{"a"}, cfg)
	got := strings.Join(m.Hints(), " · ")
	want := "↑/↓ navigate · space toggle · ctrl+a select all · type to filter · enter confirm · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("default hint sequence drifted\n got: %s\nwant: %s", got, want)
	}
}
