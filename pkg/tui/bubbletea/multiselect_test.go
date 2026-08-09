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
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// --- helpers ---

func msKey(m multiSelectModel, msg tea.KeyPressMsg) multiSelectModel {
	result, _ := m.Update(msg)
	return result.(multiSelectModel)
}

func msRune(m multiSelectModel, r rune) multiSelectModel {
	return msKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

func msCtrl(m multiSelectModel, code rune) multiSelectModel {
	return msKey(m, tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
}

func newMS(choices []string, opts ...func(*tui.MultiSelectConfig)) multiSelectModel {
	cfg := tui.MultiSelectConfig{PageSize: len(choices), Loop: true}
	for _, o := range opts {
		o(&cfg)
	}
	return newMultiSelectModel("Pick", choices, cfg)
}

// ============================================================
// Filter tests
// ============================================================

func TestMultiSelectModel_InitialFilterState(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	if m.filter != "" {
		t.Errorf("expected empty filter, got %q", m.filter)
	}
	if len(m.matched) != 3 {
		t.Errorf("expected 3 matched, got %d", len(m.matched))
	}
	for i, idx := range m.matched {
		if idx != i {
			t.Errorf("matched[%d] = %d, want %d", i, idx, i)
		}
	}
}

func TestMultiSelectModel_Refilter(t *testing.T) {
	m := newMS([]string{"Apple", "Apricot", "Banana", "Blueberry", "Cherry"})

	m.filter = "ap"
	m.refilter()

	if len(m.matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.matched))
	}
	if m.matched[0] != 0 || m.matched[1] != 1 {
		t.Errorf("matched = %v, want [0 1]", m.matched)
	}
	if m.cursor != 0 {
		t.Errorf("cursor should reset to 0, got %d", m.cursor)
	}
}

func TestMultiSelectModel_RefilterNoMatch(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	m.filter = "zzz"
	m.refilter()

	if len(m.matched) != 0 {
		t.Errorf("expected 0 matches, got %d", len(m.matched))
	}
}

func TestMultiSelectModel_RefilterClearRestoresAll(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	m.filter = "ap"
	m.refilter()
	m.filter = ""
	m.refilter()

	if len(m.matched) != 3 {
		t.Errorf("expected all 3 restored, got %d", len(m.matched))
	}
}

func TestMultiSelectModel_TypeToFilter(t *testing.T) {
	m := newMS([]string{"Apple", "Apricot", "Banana", "Blueberry"})

	m = msRune(m, 'b')

	if m.filter != "b" {
		t.Errorf("filter = %q, want %q", m.filter, "b")
	}
	if len(m.matched) != 2 {
		t.Errorf("expected 2 matches, got %d", len(m.matched))
	}
}

func TestMultiSelectModel_Backspace(t *testing.T) {
	m := newMS([]string{"Apple", "Apricot", "Banana"})

	m = msRune(m, 'a')
	m = msRune(m, 'p')

	if m.filter != "ap" {
		t.Fatalf("filter = %q, want %q", m.filter, "ap")
	}

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if m.filter != "a" {
		t.Errorf("filter after backspace = %q, want %q", m.filter, "a")
	}
	if len(m.matched) != 3 {
		t.Errorf("expected 3 matches after backspace, got %d", len(m.matched))
	}
}

func TestMultiSelectModel_BackspaceOnEmptyIsNoop(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if m.filter != "" {
		t.Errorf("filter should still be empty, got %q", m.filter)
	}
	if len(m.matched) != 2 {
		t.Errorf("expected 2 matched, got %d", len(m.matched))
	}
}

func TestMultiSelectModel_BackspaceUnicode(t *testing.T) {
	m := newMS([]string{"東京タワー", "大阪城", "富士山"})

	m = msRune(m, '東')
	m = msRune(m, '京')

	if m.filter != "東京" {
		t.Fatalf("filter = %q, want '東京'", m.filter)
	}

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if m.filter != "東" {
		t.Errorf("filter after backspace = %q, want '東'", m.filter)
	}
}

func TestMultiSelectModel_EscClearsFilterThenAborts(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	m = msRune(m, 'a')

	// First Esc — clears filter
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.filter != "" {
		t.Errorf("filter should be cleared, got %q", m.filter)
	}
	if m.aborted {
		t.Error("should not be aborted after first Esc")
	}
	if len(m.matched) != 2 {
		t.Errorf("all choices should be restored, got %d", len(m.matched))
	}

	// Second Esc — aborts
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if !m.aborted {
		t.Error("should be aborted after second Esc")
	}
}

// ============================================================
// Ctrl+A select all tests
// ============================================================

func TestMultiSelectModel_CtrlASelectsAll(t *testing.T) {
	choices := []string{"Apple", "Banana", "Cherry"}
	m := newMS(choices)

	m = msCtrl(m, 'a')

	for i := range choices {
		if !m.selected[i] {
			t.Errorf("expected choice %d (%s) to be selected", i, choices[i])
		}
	}
}

func TestMultiSelectModel_CtrlATogglesOff(t *testing.T) {
	choices := []string{"Apple", "Banana", "Cherry"}
	m := newMS(choices)

	// Select all
	m = msCtrl(m, 'a')
	// Deselect all
	m = msCtrl(m, 'a')

	if len(m.selected) != 0 {
		t.Errorf("expected 0 selected after toggle off, got %d", len(m.selected))
	}
}

func TestMultiSelectModel_CtrlASelectsOnlyVisible(t *testing.T) {
	choices := []string{"Apple", "Apricot", "Banana", "Blueberry"}
	m := newMS(choices)

	// Filter to "ap" → Apple(0), Apricot(1)
	m = msRune(m, 'a')
	m = msRune(m, 'p')

	if len(m.matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.matched))
	}

	m = msCtrl(m, 'a')

	if !m.selected[0] || !m.selected[1] {
		t.Error("Apple and Apricot should be selected")
	}
	if m.selected[2] || m.selected[3] {
		t.Error("Banana and Blueberry should NOT be selected")
	}
}

func TestMultiSelectModel_CtrlATogglesOffOnlyVisible(t *testing.T) {
	choices := []string{"Apple", "Apricot", "Banana", "Blueberry"}
	m := newMS(choices)

	// Select all first
	m = msCtrl(m, 'a')

	// Filter to "b" → Banana(2), Blueberry(3)
	m = msRune(m, 'b')

	// Toggle off visible only
	m = msCtrl(m, 'a')

	// Apple and Apricot should still be selected
	if !m.selected[0] || !m.selected[1] {
		t.Error("Apple and Apricot should still be selected")
	}
	// Banana and Blueberry should be deselected
	if m.selected[2] || m.selected[3] {
		t.Error("Banana and Blueberry should be deselected")
	}
}

func TestMultiSelectModel_CtrlARespectsMax(t *testing.T) {
	choices := []string{"Apple", "Banana", "Cherry", "Date"}
	m := newMS(choices, func(c *tui.MultiSelectConfig) { c.Max = 2 })

	m = msCtrl(m, 'a')

	count := 0
	for range m.selected {
		count++
	}
	if count > 2 {
		t.Errorf("expected at most 2 selected, got %d", count)
	}
	if m.err == "" {
		t.Error("expected error message about max selections")
	}
}

// ============================================================
// Navigation & selection tests
// ============================================================

func TestMultiSelectModel_ArrowNavigation(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.cursor)
	}

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", m.cursor)
	}
}

func TestMultiSelectModel_ArrowWrapsWithLoop(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	// Up from top wraps to bottom
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 2 {
		t.Errorf("cursor after wrap up = %d, want 2", m.cursor)
	}

	// Down from bottom wraps to top
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 0 {
		t.Errorf("cursor after wrap down = %d, want 0", m.cursor)
	}
}

func TestMultiSelectModel_VimKeysWithoutFilter(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	m = msRune(m, 'j')
	if m.cursor != 1 {
		t.Errorf("j should move down: cursor = %d, want 1", m.cursor)
	}
	if m.filter != "" {
		t.Errorf("j should not set filter, got %q", m.filter)
	}

	m = msRune(m, 'k')
	if m.cursor != 0 {
		t.Errorf("k should move up: cursor = %d, want 0", m.cursor)
	}
}

func TestMultiSelectModel_VimKeysTypeWhenFiltering(t *testing.T) {
	m := newMS([]string{"Ajax", "Jolt", "Koji"})

	// Start typing — 'a' begins filter, then 'j' goes into filter
	m = msRune(m, 'a')
	m = msRune(m, 'j')

	if m.filter != "aj" {
		t.Errorf("filter = %q, want %q", m.filter, "aj")
	}
}

func TestMultiSelectModel_ArrowKeysOnEmptyMatches(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	for _, r := range "zzz" {
		m = msRune(m, r)
	}
	if len(m.matched) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(m.matched))
	}

	// Should not panic
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyUp})

	if m.cursor != 0 {
		t.Errorf("cursor should remain 0, got %d", m.cursor)
	}
}

func TestMultiSelectModel_SpaceTogglesSelection(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	// Select first item
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if !m.selected[0] {
		t.Error("Apple should be selected")
	}

	// Deselect it
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.selected[0] {
		t.Error("Apple should be deselected")
	}
}

func TestMultiSelectModel_SpaceRespectsMax(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"}, func(c *tui.MultiSelectConfig) { c.Max = 1 })

	// Select first
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	// Move down, try to select second
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})

	if m.selected[1] {
		t.Error("Banana should NOT be selected — max is 1")
	}
	if m.err == "" {
		t.Error("expected error message about max selections")
	}
}

func TestMultiSelectModel_SpaceOnEmptyMatchesIsNoop(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	for _, r := range "zzz" {
		m = msRune(m, r)
	}

	// Space on no matches should not panic
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})

	if len(m.selected) != 0 {
		t.Errorf("no items should be selected, got %d", len(m.selected))
	}
}

func TestMultiSelectModel_EnterConfirms(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	// Select Apple and Cherry
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !m.done {
		t.Error("expected done=true")
	}
	if !m.selected[0] || !m.selected[2] {
		t.Error("Apple(0) and Cherry(2) should be selected")
	}
	if m.selected[1] {
		t.Error("Banana(1) should not be selected")
	}
}

func TestMultiSelectModel_EnterRejectsIfBelowMin(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"}, func(c *tui.MultiSelectConfig) { c.Min = 2 })

	// Select only one
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})

	result, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(multiSelectModel)

	if m.done {
		t.Error("should not be done — min not met")
	}
	if cmd != nil {
		t.Error("should not quit when min not met")
	}
	if m.err == "" {
		t.Error("expected error about minimum selections")
	}
}

func TestMultiSelectModel_MinErrorOverride(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"},
		func(c *tui.MultiSelectConfig) { c.Min = 2 },
		func(c *tui.MultiSelectConfig) {
			c.MinError = func(min int) string { return fmt.Sprintf("pick %d, friend", min) }
		},
	)

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace}) // one selection
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.err != "pick 2, friend" {
		t.Errorf("expected overridden min error, got %q", m.err)
	}
}

func TestMultiSelectModel_MaxErrorOverride(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"},
		func(c *tui.MultiSelectConfig) { c.Max = 1 },
		func(c *tui.MultiSelectConfig) {
			c.MaxError = func(max int) string { return fmt.Sprintf("only %d allowed!", max) }
		},
	)

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace}) // first
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace}) // second — rejected

	if m.err != "only 1 allowed!" {
		t.Errorf("expected overridden max error, got %q", m.err)
	}
}

func TestMultiSelectModel_MaxErrorOverride_ViaToggleAll(t *testing.T) {
	// select-all (ctrl+a) is a separate code path from space-toggle; it must
	// honor the override too.
	m := newMS([]string{"Apple", "Banana", "Cherry"},
		func(c *tui.MultiSelectConfig) { c.Max = 2 },
		func(c *tui.MultiSelectConfig) {
			c.MaxError = func(max int) string { return fmt.Sprintf("cap is %d", max) }
		},
	)

	m = msCtrl(m, 'a') // tries to select all 3, hits max at 2

	if m.err != "cap is 2" {
		t.Errorf("toggleAll path: expected overridden max error, got %q", m.err)
	}
}

func TestMultiSelectModel_MinErrorDefaultWhenUnset(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"}, func(c *tui.MultiSelectConfig) { c.Min = 2 })

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !strings.Contains(m.err, "at least 2 selections required") {
		t.Errorf("expected default min error, got %q", m.err)
	}
}

func TestMultiSelectModel_MaxErrorDefaultWhenUnset(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"}, func(c *tui.MultiSelectConfig) { c.Max = 1 })

	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace}) // rejected — over max

	if !strings.Contains(m.err, "maximum 1 selections allowed") {
		t.Errorf("expected default max error, got %q", m.err)
	}
}

func TestMultiSelectModel_CtrlCInterrupts(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	m = msCtrl(m, 'c')

	if !m.interrupted {
		t.Error("ctrl+c should set interrupted")
	}
}

func TestMultiSelectModel_DefaultSelections(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"}, func(c *tui.MultiSelectConfig) {
		c.Defaults = []int{0, 2}
	})

	if !m.selected[0] || !m.selected[2] {
		t.Error("defaults 0 and 2 should be selected")
	}
	if m.selected[1] {
		t.Error("index 1 should not be selected")
	}
}

func TestMultiSelectModel_NavigationInFilteredList(t *testing.T) {
	m := newMS([]string{"Apple", "Apricot", "Banana", "Avocado"})

	// Filter to "ap" → Apple(0), Apricot(1)
	m = msRune(m, 'a')
	m = msRune(m, 'p')

	if len(m.matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.matched))
	}

	// Move down within filtered list
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	// Space selects the correct original item (Apricot = index 1)
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if !m.selected[1] {
		t.Error("Apricot (original index 1) should be selected")
	}
	if m.selected[0] {
		t.Error("Apple should not be selected")
	}
}

// ============================================================
// View rendering tests
// ============================================================

func TestMultiSelectModel_ViewShowsFilter(t *testing.T) {
	m := newMS([]string{"Apple", "Apricot", "Banana"})

	m = msRune(m, 'a')
	m = msRune(m, 'p')

	view := m.View().Content

	if !strings.Contains(view, "ap") {
		t.Errorf("view should show filter text 'ap', got:\n%s", view)
	}
	if !strings.Contains(view, "Apple") {
		t.Error("view should show Apple")
	}
	if !strings.Contains(view, "Apricot") {
		t.Error("view should show Apricot")
	}
	if strings.Contains(view, "Banana") {
		t.Error("view should NOT show Banana")
	}
}

func TestMultiSelectModel_ViewNoMatchMessage(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"})

	m = msRune(m, 'z')

	view := m.View().Content
	if !strings.Contains(view, "no matches") {
		t.Errorf("view should show 'no matches', got:\n%s", view)
	}
}

func TestMultiSelectModel_ViewShowsCheckmarks(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	// Select Apple
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})

	view := m.View().Content
	if !strings.Contains(view, "[x]") {
		t.Error("view should show [x] for selected item")
	}
	if !strings.Contains(view, "[ ]") {
		t.Error("view should show [ ] for unselected items")
	}
}

func TestMultiSelectModel_ViewDoneShowsSelected(t *testing.T) {
	m := newMS([]string{"Apple", "Banana", "Cherry"})

	// Select Apple and Cherry
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := m.View().Content
	if !strings.Contains(view, "Apple") || !strings.Contains(view, "Cherry") {
		t.Errorf("done view should show selected items, got:\n%s", view)
	}
	// Should not show checkboxes in done view
	if strings.Contains(view, "[x]") || strings.Contains(view, "[ ]") {
		t.Error("done view should not show checkboxes")
	}
}

func TestMultiSelectModel_View_HintBar(t *testing.T) {
	const defaultBar = "↑/↓ navigate · space toggle · ctrl+a select all · type to filter · enter confirm · esc back · ctrl+c exit"
	tests := []struct {
		name        string
		mutate      func(*tui.MultiSelectConfig)
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:       "hints off by default",
			mutate:     func(c *tui.MultiSelectConfig) {},
			wantAbsent: []string{"navigate", "ctrl+c exit"},
		},
		{
			name:        "ShowHints renders default bar",
			mutate:      func(c *tui.MultiSelectConfig) { c.ShowHints = true },
			wantContain: []string{defaultBar},
		},
		{
			name: "WithMultiSelectHints overrides defaults",
			mutate: func(c *tui.MultiSelectConfig) {
				c.ShowHints = true
				c.Hints = []string{"↑/↓ move", "␣ check", "↵ done"}
			},
			wantContain: []string{"↑/↓ move · ␣ check · ↵ done"},
			wantAbsent:  []string{"navigate", "ctrl+a", "ctrl+c exit"},
		},
		{
			name: "override ignored when ShowHints off",
			mutate: func(c *tui.MultiSelectConfig) {
				c.Hints = []string{"x"}
			},
			wantAbsent: []string{"navigate", "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMS([]string{"a", "b"}, tt.mutate)
			view := m.View().Content
			for _, s := range tt.wantContain {
				if !strings.Contains(view, s) {
					t.Errorf("expected view to contain %q, got:\n%s", s, view)
				}
			}
			for _, s := range tt.wantAbsent {
				if strings.Contains(view, s) {
					t.Errorf("expected view to NOT contain %q, got:\n%s", s, view)
				}
			}
		})
	}
}

func TestMultiSelectModel_HintBar_AbsentInDoneView(t *testing.T) {
	m := newMS([]string{"alpha", "beta"}, func(c *tui.MultiSelectConfig) { c.ShowHints = true })
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done {
		t.Fatal("expected done=true")
	}
	view := m.View().Content
	if strings.Contains(view, "navigate") {
		t.Errorf("done view must not render hint bar, got:\n%s", view)
	}
}

func TestMultiSelectModel_HintBar_AtBottomBelowChoices(t *testing.T) {
	m := newMS([]string{"alpha", "beta"}, func(c *tui.MultiSelectConfig) { c.ShowHints = true })
	view := m.View().Content
	choicesAt := strings.Index(view, "alpha")
	hintsAt := strings.Index(view, "↑/↓ navigate")
	if choicesAt < 0 || hintsAt < 0 {
		t.Fatalf("expected both choices and hint bar in view, got:\n%s", view)
	}
	if hintsAt <= choicesAt {
		t.Errorf("hint bar should be after choices: choices@%d hints@%d", choicesAt, hintsAt)
	}
}

func TestMultiSelectModel_ViewShowsError(t *testing.T) {
	m := newMS([]string{"Apple", "Banana"}, func(c *tui.MultiSelectConfig) { c.Min = 2 })

	// Try to confirm with 0 selected
	m = msKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := m.View().Content
	if !strings.Contains(view, "at least 2") {
		t.Errorf("view should show min error, got:\n%s", view)
	}
}
