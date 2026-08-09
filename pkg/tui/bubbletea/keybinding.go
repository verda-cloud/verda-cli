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

	tea "charm.land/bubbletea/v2"
)

// KeyBinding pairs a matcher with a state-aware label and handler.
// Generic over M so handlers mutate state directly; empty Label hides
// the entry from the hint bar without disabling Handle.
type KeyBinding[M any] struct {
	// Stable kebab-case identifier for relabel/hide overrides.
	ID string

	// Pure function of msg; must not inspect model state.
	Match func(tea.KeyPressMsg) bool

	// Dynamic hint text; "" hides this entry.
	Label func(*M) string

	// Returns (cmd, stop). stop=false falls through to next matching binding.
	Handle func(*M, tea.KeyPressMsg) (tea.Cmd, bool)
}

// MatchKey matches any of the given key codes. bubbletea v2's named
// key constants (KeyUp, KeyEnter, …) are typed rune.
func MatchKey(codes ...rune) func(tea.KeyPressMsg) bool {
	return func(msg tea.KeyPressMsg) bool {
		return slices.Contains(codes, msg.Code)
	}
}

// MatchRune matches r, optionally constrained by mod. With no mod
// the binding fires only when no modifier is held — MatchRune('k')
// won't match Ctrl+K.
func MatchRune(r rune, mod ...tea.KeyMod) func(tea.KeyPressMsg) bool {
	var required tea.KeyMod
	for _, m := range mod {
		required |= m
	}
	return func(msg tea.KeyPressMsg) bool {
		if msg.Code != r {
			return false
		}
		if required == 0 {
			return msg.Mod == 0
		}
		return msg.Mod&required == required
	}
}

// MatchText fires on any key event carrying printable text. Order after
// specific rune matchers so they get first claim.
func MatchText() func(tea.KeyPressMsg) bool {
	return func(msg tea.KeyPressMsg) bool { return msg.Text != "" }
}

// Dispatch returns the cmd of the first matching binding whose Handle
// returns stop=true. A handler may return (nil, false) to fall through
// to the next matching binding even when its Match fired.
func Dispatch[M any](m *M, bindings []KeyBinding[M], msg tea.KeyPressMsg) (tea.Cmd, bool) {
	for _, b := range bindings {
		if b.Match == nil || b.Handle == nil {
			continue
		}
		if !b.Match(msg) {
			continue
		}
		cmd, stop := b.Handle(m, msg)
		if stop {
			return cmd, true
		}
	}
	return nil, false
}

// HintsFor returns non-empty Label values in declaration order.
func HintsFor[M any](m *M, bindings []KeyBinding[M]) []string {
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if b.Label == nil {
			continue
		}
		if lbl := b.Label(m); lbl != "" {
			out = append(out, lbl)
		}
	}
	return out
}

// ApplyBindingOverrides returns a copy of bindings with per-ID relabels
// applied and hidden IDs replaced by empty-label closures. The input
// slice is not mutated.
func ApplyBindingOverrides[M any](bindings []KeyBinding[M], relabels map[string]string, hidden []string) []KeyBinding[M] {
	if len(relabels) == 0 && len(hidden) == 0 {
		return bindings
	}
	hiddenSet := make(map[string]struct{}, len(hidden))
	for _, id := range hidden {
		hiddenSet[id] = struct{}{}
	}
	out := make([]KeyBinding[M], len(bindings))
	for i, b := range bindings {
		out[i] = b
		if _, ok := hiddenSet[b.ID]; ok {
			out[i].Label = func(*M) string { return "" }
			continue
		}
		if lbl, ok := relabels[b.ID]; ok {
			label := lbl
			out[i].Label = func(*M) string { return label }
		}
	}
	return out
}
