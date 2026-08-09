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

import tea "charm.land/bubbletea/v2"

// Action represents a wizard-level command triggered by a key binding.
type Action int

const (
	ActionExit Action = iota // exit the wizard
	ActionBack               // go to previous step (reserved for future use)
)

// KeyPattern matches a tea.KeyPressMsg.
type KeyPattern struct {
	Code rune
	Mod  tea.KeyMod
}

// KeyBinding maps a key pattern to a wizard-level action.
type KeyBinding struct {
	Key    KeyPattern
	Action Action
	Label  string // displayed in hint bar
}

// DefaultKeyBindings returns the default wizard key bindings.
// The Ctrl+C binding's Label is empty so the prompt's Hints() owns
// the "ctrl+c exit" display — the composite concatenates without
// dedup, and a label here would duplicate it.
func DefaultKeyBindings() []KeyBinding {
	return []KeyBinding{
		{Key: KeyPattern{Code: 'c', Mod: tea.ModCtrl}, Action: ActionExit, Label: ""},
	}
}

// MatchBinding checks if a key message matches any binding.
// Returns the action and true if matched, or zero and false if not.
func MatchBinding(bindings []KeyBinding, msg tea.KeyPressMsg) (Action, bool) {
	for _, b := range bindings {
		if msg.Code == b.Key.Code && msg.Mod == b.Key.Mod {
			return b.Action, true
		}
	}
	return 0, false
}
