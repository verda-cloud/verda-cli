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
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/bubbletea"
)

func TestComposite_CtrlC_ProducesExit(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"a", "b"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	// Send Ctrl+C
	_, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	select {
	case r := <-resultCh:
		if r.action != ActionExit {
			t.Errorf("expected ActionExit, got %v", r.action)
		}
	default:
		t.Fatal("expected result on channel")
	}
}

func TestComposite_Enter_ForwardedToPrompt(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"a"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	// Enter on first item
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case r := <-resultCh:
		if r.action != ActionNone {
			t.Errorf("expected ActionNone (success), got %v", r.action)
		}
		if r.value != 0 { // index 0
			t.Errorf("expected value 0, got %v", r.value)
		}
	default:
		t.Fatal("expected result on channel")
	}
}

func TestComposite_GoBackMsg_ProducesBack(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"a"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	// Esc with no filter → GoBackMsg
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(compositeModel)

	// The cmd from the prompt returns GoBackMsg — deliver it back
	if cmd != nil {
		msg := cmd()
		_, _ = m.Update(msg)
	}

	select {
	case r := <-resultCh:
		if r.action != ActionBack {
			t.Errorf("expected ActionBack, got %v", r.action)
		}
	default:
		t.Fatal("expected result on channel")
	}
}

func TestComposite_View_ContainsPrompt(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("Pick color", []string{"red", "blue"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	view := m.View()
	if view.Content == "" {
		t.Fatal("expected non-empty view")
	}
}

// TestComposite_HintBar_UsesPromptHints verifies the wizard's composite
// hint bar reads from prompt.Hints() and renders default hints (without
// duplicate ctrl+c — DefaultKeyBindings now suppresses its label).
func TestComposite_HintBar_UsesPromptHints(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil) // ShowHints = false (wizard default)
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"a", "b"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	view := m.View().Content
	if !strings.Contains(view, "↑/↓ navigate") {
		t.Errorf("wizard hint bar missing default hints, got:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+c exit") {
		t.Errorf("wizard hint bar should show ctrl+c exit (via prompt.Hints), got:\n%s", view)
	}
	// Must not appear twice (prompt owns it, wizard label is empty).
	if strings.Count(view, "ctrl+c exit") != 1 {
		t.Errorf("ctrl+c exit should appear exactly once, got %d:\n%s",
			strings.Count(view, "ctrl+c exit"), view)
	}
	// In the wizard path, ShowHints stays false → prompt does not render
	// its own internal hint bar inside View.Content. So default hints
	// appear once total (from the composite).
	if strings.Count(view, "↑/↓ navigate") != 1 {
		t.Errorf("default hints should appear exactly once in wizard view, got %d:\n%s",
			strings.Count(view, "↑/↓ navigate"), view)
	}
}

// TestComposite_HintBar_HonorsPromptOverride verifies callers can swap the
// hint text in a wizard step by passing WithHints when constructing the
// prompt — composite reads from prompt.Hints() so the override propagates.
func TestComposite_HintBar_HonorsPromptOverride(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	custom := []string{"↑/↓ pick", "↵ go"}
	cfg := tui.ResolveSelectConfig([]tui.SelectOption{tui.WithHints(custom...)})
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"a"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	view := m.View().Content
	if !strings.Contains(view, "↑/↓ pick · ↵ go") {
		t.Errorf("prompt override did not propagate to wizard hint bar, got:\n%s", view)
	}
	if strings.Contains(view, "↑/↓ navigate") {
		t.Errorf("default hints should be replaced, got:\n%s", view)
	}
}

// TestComposite_HintBar_RefreshesOnFilter verifies the composite hint
// bar reflects dynamic prompt-hint labels as the prompt's state changes.
// The select esc binding flips "esc back" → "esc clear filter" once a
// filter is active; the composite must re-read Hints() per update rather
// than snapshotting once at setPrompt.
func TestComposite_HintBar_RefreshesOnFilter(t *testing.T) {
	resultCh := make(chan promptResult, 1)
	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("Pick", []string{"alpha", "beta"}, cfg)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)
	m.setPrompt(prompt)

	view := m.View().Content
	if !strings.Contains(view, "esc back") {
		t.Fatalf("setup: expected 'esc back' before filtering, got:\n%s", view)
	}

	// Type a character to start a filter.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(compositeModel)

	view = m.View().Content
	if !strings.Contains(view, "esc clear filter") {
		t.Errorf("hint bar did not refresh after filtering, got:\n%s", view)
	}
	if strings.Contains(view, "esc back") {
		t.Errorf("stale 'esc back' label still present after filtering, got:\n%s", view)
	}
}

func TestComposite_ShowPromptMsg_SwapsPrompt(t *testing.T) {
	resultCh := make(chan promptResult, 1)

	m := newCompositeModel(DefaultKeyBindings(), nil, resultCh)

	cfg := tui.ResolveSelectConfig(nil)
	prompt := bubbletea.NewSelectPrompt("New prompt", []string{"x"}, cfg)

	updated, _ := m.Update(showPromptMsg{model: prompt})
	m = updated.(compositeModel)

	if m.prompt == nil {
		t.Fatal("expected prompt to be set after showPromptMsg")
	}
}
