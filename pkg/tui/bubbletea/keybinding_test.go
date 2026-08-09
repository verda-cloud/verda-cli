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
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMatchKey_SingleAndMulti(t *testing.T) {
	upOnly := MatchKey(tea.KeyUp)
	upDown := MatchKey(tea.KeyUp, tea.KeyDown)

	if !upOnly(tea.KeyPressMsg{Code: tea.KeyUp}) {
		t.Error("upOnly should match KeyUp")
	}
	if upOnly(tea.KeyPressMsg{Code: tea.KeyDown}) {
		t.Error("upOnly should NOT match KeyDown")
	}
	if !upDown(tea.KeyPressMsg{Code: tea.KeyDown}) {
		t.Error("upDown should match KeyDown")
	}
	if upDown(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Error("upDown should NOT match KeyEnter")
	}
}

func TestMatchRune_PlainAndModifier(t *testing.T) {
	plainK := MatchRune('k')
	ctrlC := MatchRune('c', tea.ModCtrl)

	if !plainK(tea.KeyPressMsg{Code: 'k'}) {
		t.Error("plainK should match plain 'k'")
	}
	if plainK(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}) {
		t.Error("plainK should NOT match Ctrl+K (modifier should disqualify)")
	}
	if !ctrlC(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) {
		t.Error("ctrlC should match Ctrl+C")
	}
	if ctrlC(tea.KeyPressMsg{Code: 'c'}) {
		t.Error("ctrlC should NOT match plain 'c'")
	}
}

func TestMatchText_OnlyMatchesPrintable(t *testing.T) {
	mt := MatchText()
	if mt(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Error("MatchText should NOT match Enter (no Text)")
	}
	if !mt(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Error("MatchText should match printable 'a'")
	}
}

// Dispatch covers: first-match wins, pass-through (stop=false),
// stop=true short-circuits, no-match returns (nil, false).
func TestDispatch_OrderingAndStop(t *testing.T) {
	type state struct {
		ran []string
	}

	calls := func(id string) func(*state, tea.KeyPressMsg) (tea.Cmd, bool) {
		return func(s *state, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			s.ran = append(s.ran, id)
			return nil, true
		}
	}
	pass := func(id string) func(*state, tea.KeyPressMsg) (tea.Cmd, bool) {
		return func(s *state, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			s.ran = append(s.ran, id+"-pass")
			return nil, false
		}
	}

	bindings := []KeyBinding[state]{
		{ID: "first", Match: MatchKey(tea.KeyEnter), Handle: pass("first")},
		{ID: "second", Match: MatchKey(tea.KeyEnter), Handle: calls("second")},
		{ID: "third", Match: MatchKey(tea.KeyEnter), Handle: calls("third")},
	}

	var s state
	_, ok := Dispatch(&s, bindings, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !ok {
		t.Fatal("expected dispatch to claim Enter")
	}
	if !reflect.DeepEqual(s.ran, []string{"first-pass", "second"}) {
		t.Errorf("expected first-pass→second, got %v", s.ran)
	}
}

func TestDispatch_NoMatchReturnsNotClaimed(t *testing.T) {
	type state struct{}
	bindings := []KeyBinding[state]{
		{ID: "e", Match: MatchKey(tea.KeyEnter), Handle: func(*state, tea.KeyPressMsg) (tea.Cmd, bool) { return nil, true }},
	}
	_, ok := Dispatch(&state{}, bindings, tea.KeyPressMsg{Code: tea.KeyEscape})
	if ok {
		t.Error("expected unclaimed when no binding matches")
	}
}

func TestDispatch_NilSafeMatchAndHandle(t *testing.T) {
	type state struct{}
	bindings := []KeyBinding[state]{
		{ID: "broken-match", Handle: func(*state, tea.KeyPressMsg) (tea.Cmd, bool) { return nil, true }},
		{ID: "broken-handle", Match: MatchKey(tea.KeyEnter)},
		{ID: "ok", Match: MatchKey(tea.KeyEnter), Handle: func(*state, tea.KeyPressMsg) (tea.Cmd, bool) { return nil, true }},
	}
	_, ok := Dispatch(&state{}, bindings, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !ok {
		t.Error("expected dispatch to find a fully-formed binding")
	}
}

func TestHintsFor_FiltersEmptyLabels(t *testing.T) {
	type state struct {
		filter string
	}
	bindings := []KeyBinding[state]{
		{ID: "nav", Label: func(*state) string { return "↑/↓ navigate" }},
		{ID: "esc", Label: func(s *state) string {
			if s.filter != "" {
				return "esc clear filter"
			}
			return "esc back"
		}},
		{ID: "hidden", Label: func(*state) string { return "" }},
		{ID: "no-label-fn"}, // nil Label safely skipped
	}

	got := HintsFor(&state{}, bindings)
	if !reflect.DeepEqual(got, []string{"↑/↓ navigate", "esc back"}) {
		t.Errorf("default state hints = %v", got)
	}
	got = HintsFor(&state{filter: "a"}, bindings)
	if !reflect.DeepEqual(got, []string{"↑/↓ navigate", "esc clear filter"}) {
		t.Errorf("filtering state hints = %v", got)
	}
}

func TestApplyBindingOverrides_RelabelAndHide(t *testing.T) {
	type state struct{}
	defaults := []KeyBinding[state]{
		{ID: "nav", Label: func(*state) string { return "↑/↓ navigate" }},
		{ID: "esc", Label: func(*state) string { return "esc back" }},
		{ID: "exit", Label: func(*state) string { return "ctrl+c exit" }},
	}

	overridden := ApplyBindingOverrides(defaults, map[string]string{"esc": "esc cancel"}, []string{"exit"})

	hints := HintsFor(&state{}, overridden)
	if !reflect.DeepEqual(hints, []string{"↑/↓ navigate", "esc cancel"}) {
		t.Errorf("expected relabel + hide, got %v", hints)
	}

	// Defaults must not be mutated.
	if defaults[1].Label(&state{}) != "esc back" {
		t.Error("defaults were mutated in place")
	}
	if defaults[2].Label(&state{}) != "ctrl+c exit" {
		t.Error("hidden binding label was mutated in defaults slice")
	}
}

func TestApplyBindingOverrides_NoopWhenEmpty(t *testing.T) {
	type state struct{}
	defaults := []KeyBinding[state]{
		{ID: "a", Label: func(*state) string { return "a" }},
	}
	got := ApplyBindingOverrides(defaults, nil, nil)
	// Identity return is fine; behavior must be equivalent.
	if HintsFor(&state{}, got)[0] != "a" {
		t.Error("identity passthrough broken")
	}
}
