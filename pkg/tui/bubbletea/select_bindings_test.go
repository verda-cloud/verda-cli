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
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// Dynamic label: Esc reads "esc back" with empty filter, "esc clear
// filter" when filter is active. This is the headline payoff of the
// binding refactor — labels are now closures over model state.
func TestSelectBindings_DynamicEscLabel(t *testing.T) {
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{tui.WithShowHints(true)})
	m := newSelectModel("Pick", []string{"alpha", "beta"}, cfg)

	// No filter yet → "esc back".
	hints := m.Hints()
	if !containsString(hints, "esc back") {
		t.Errorf("expected 'esc back' before filtering, got %v", hints)
	}
	if containsString(hints, "esc clear filter") {
		t.Errorf("did not expect 'esc clear filter' before filtering, got %v", hints)
	}

	// Type a filter char.
	m = sendRune(m, 'a')
	hints = m.Hints()
	if !containsString(hints, "esc clear filter") {
		t.Errorf("expected 'esc clear filter' while filtering, got %v", hints)
	}
	if containsString(hints, "esc back") {
		t.Errorf("did not expect 'esc back' while filtering, got %v", hints)
	}

	// Clear filter via Esc → back to "esc back".
	m = sendKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	hints = m.Hints()
	if !containsString(hints, "esc back") {
		t.Errorf("expected 'esc back' after filter cleared, got %v", hints)
	}
}

// View renders the dynamic label too — not just Hints().
func TestSelectBindings_DynamicEscLabel_InView(t *testing.T) {
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{tui.WithShowHints(true)})
	m := newSelectModel("Pick", []string{"alpha", "beta"}, cfg)
	m = sendRune(m, 'a')

	view := m.View().Content
	if !strings.Contains(view, "esc clear filter") {
		t.Errorf("expected dynamic label in view, got:\n%s", view)
	}
	if strings.Contains(view, "esc back") {
		t.Errorf("expected old label NOT in view while filtering, got:\n%s", view)
	}
}

// WithSelectRelabel renames a binding by ID without touching key handling.
func TestSelectBindings_WithSelectRelabel(t *testing.T) {
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{
		tui.WithShowHints(true),
		tui.WithSelectRelabel("esc", "esc cancel"),
		tui.WithSelectRelabel("select", "↵ open"),
	})
	m := newSelectModel("Pick", []string{"alpha"}, cfg)

	hints := m.Hints()
	if !containsString(hints, "esc cancel") {
		t.Errorf("expected relabeled esc, got %v", hints)
	}
	if !containsString(hints, "↵ open") {
		t.Errorf("expected relabeled select, got %v", hints)
	}
	if containsString(hints, "esc back") {
		t.Errorf("relabel did not replace default, got %v", hints)
	}

	// Key handling still works — Enter completes the prompt.
	m = sendKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chosen {
		t.Error("Enter should still complete after relabel")
	}
}

// WithSelectHide removes labels but keeps key handling.
func TestSelectBindings_WithSelectHide(t *testing.T) {
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{
		tui.WithShowHints(true),
		tui.WithSelectHide("exit", "select"),
	})
	m := newSelectModel("Pick", []string{"alpha"}, cfg)

	hints := m.Hints()
	if containsString(hints, "ctrl+c exit") {
		t.Errorf("exit should be hidden, got %v", hints)
	}
	if containsString(hints, "enter select") {
		t.Errorf("select should be hidden, got %v", hints)
	}
	if !containsString(hints, "↑/↓ navigate") {
		t.Errorf("other defaults must remain, got %v", hints)
	}

	// Ctrl+C still triggers exit (sets interrupted).
	m = sendKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.interrupted {
		t.Error("ctrl+c should still interrupt even when hidden")
	}
}

// WithSelectAddBindings appends a custom binding (e.g. '?' help).
// The new binding both renders its label and triggers its handler.
func TestSelectBindings_WithSelectAddBindings(t *testing.T) {
	helped := false
	help := KeyBinding[selectModel]{
		ID:    "help",
		Match: MatchRune('?'),
		Label: func(*selectModel) string { return "? help" },
		Handle: func(_ *selectModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			helped = true
			return nil, true
		},
	}
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{
		tui.WithShowHints(true),
		WithSelectAddBindings(help),
	})
	m := newSelectModel("Pick", []string{"alpha"}, cfg)

	if !containsString(m.Hints(), "? help") {
		t.Errorf("custom binding label missing, got %v", m.Hints())
	}

	m = sendRune(m, '?')
	if !helped {
		t.Error("custom binding handler did not fire")
	}
	// Filter must NOT have absorbed '?' (custom binding stopped dispatch).
	if m.filter == "?" {
		t.Error("custom binding should have stopped before filter-type")
	}
}

// Default binding order yields the historical hint string verbatim.
// Locks in compatibility for any consumer asserting on display order.
func TestSelectBindings_DefaultHintsOrder(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := newSelectModel("Pick", []string{"a"}, cfg)
	got := strings.Join(m.Hints(), " · ")
	want := "↑/↓ navigate · type to filter · enter select · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("default hint sequence drifted\n got: %s\nwant: %s", got, want)
	}
}

// Pressing Esc in standalone mode must actually quit the bubbletea
// program (via tea.Quit) so Prompter.Select returns context.Canceled.
// In wizard mode the composite intercepts GoBackMsg before it reaches
// the prompt, so this test specifically targets the standalone path.
func TestSelectBindings_EscQuitsInStandaloneMode(t *testing.T) {
	cfg := tui.ResolveSelectConfig(nil)
	m := newSelectModel("Pick", []string{"a", "b"}, cfg)

	// 1. Esc with no filter → sets aborted, emits GoBackMsg cmd.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(selectModel)
	if !got.aborted {
		t.Fatal("Esc should set aborted=true")
	}
	if cmd == nil {
		t.Fatal("Esc should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(GoBackMsg); !ok {
		t.Fatalf("Esc cmd should emit GoBackMsg, got %T", msg)
	}

	// 2. Feeding GoBackMsg back into Update should yield tea.Quit so
	//    the program terminates instead of looping forever.
	_, quitCmd := got.Update(GoBackMsg{})
	if quitCmd == nil {
		t.Fatal("GoBackMsg should produce a cmd")
	}
	if _, isQuit := quitCmd().(tea.QuitMsg); !isQuit {
		t.Errorf("GoBackMsg should produce tea.QuitMsg, got %T", quitCmd())
	}
}

// containsString is a small helper to keep assertions readable.
func containsString(xs []string, s string) bool {
	return slices.Contains(xs, s)
}
