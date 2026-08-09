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
)

func TestPasswordBindings_DefaultHintsOrder(t *testing.T) {
	m := newPasswordModel("Token?")
	got := strings.Join(m.Hints(), " · ")
	want := "enter submit · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("default hint order drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestPasswordBindings_EnterSubmits(t *testing.T) {
	m := newPasswordModel("Token?")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !updated.(passwordModel).submitted {
		t.Error("enter should submit")
	}
}

func TestPasswordBindings_EscAborts(t *testing.T) {
	m := newPasswordModel("Token?")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !updated.(passwordModel).aborted {
		t.Error("esc should mark aborted")
	}
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	if _, ok := cmd().(GoBackMsg); !ok {
		t.Error("esc should produce GoBackMsg")
	}
}

func TestPasswordBindings_CtrlCInterrupts(t *testing.T) {
	m := newPasswordModel("Token?")
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.(passwordModel).interrupted {
		t.Error("ctrl+c should set interrupted")
	}
}

// Printable text falls through to the bubbles textinput, populating
// the underlying value (which is echoed as bullets).
func TestPasswordBindings_TypingFallsThroughToTextinput(t *testing.T) {
	m := newPasswordModel("Token?")
	for _, r := range "secret" {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(passwordModel)
	}
	if m.textInput.Value() != "secret" {
		t.Errorf("expected underlying value 'secret', got %q", m.textInput.Value())
	}
}
