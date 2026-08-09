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

func TestConfirmBindings_DefaultHintsOrder(t *testing.T) {
	cfg := tui.ResolveConfirmConfig(nil)
	m := newConfirmModel("Sure?", cfg)
	got := strings.Join(m.Hints(), " · ")
	want := "y/n · enter confirm · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("default hint order drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestConfirmBindings_YesAndNo(t *testing.T) {
	for _, tc := range []struct {
		text   string
		expect bool
	}{
		{"y", true},
		{"Y", true},
		{"n", false},
		{"N", false},
	} {
		m := newConfirmModel("Sure?", tui.ResolveConfirmConfig(nil))
		updated, _ := m.Update(tea.KeyPressMsg{Code: rune(tc.text[0]), Text: tc.text})
		got := updated.(confirmModel)
		if !got.decided {
			t.Errorf("text=%q expected decided", tc.text)
		}
		if got.value != tc.expect {
			t.Errorf("text=%q value = %v, want %v", tc.text, got.value, tc.expect)
		}
	}
}

func TestConfirmBindings_EnterUsesDefault(t *testing.T) {
	cfg := tui.ResolveConfirmConfig([]tui.ConfirmOption{tui.WithConfirmDefault(true)})
	m := newConfirmModel("Sure?", cfg)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(confirmModel)
	if !got.decided || !got.value {
		t.Errorf("enter should commit default true, got decided=%v value=%v", got.decided, got.value)
	}
}

func TestConfirmBindings_CtrlCInterrupts(t *testing.T) {
	m := newConfirmModel("Sure?", tui.ResolveConfirmConfig(nil))
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	got := updated.(confirmModel)
	if !got.interrupted {
		t.Error("ctrl+c should set interrupted")
	}
}

func TestConfirmBindings_Relabel(t *testing.T) {
	cfg := tui.ResolveConfirmConfig([]tui.ConfirmOption{
		tui.WithConfirmRelabel("yes-no", "Y / N"),
		tui.WithConfirmRelabel("confirm", "↵ ok"),
	})
	m := newConfirmModel("Sure?", cfg)
	got := strings.Join(m.Hints(), " · ")
	want := "Y / N · ↵ ok · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("relabel mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestConfirmBindings_Hide(t *testing.T) {
	cfg := tui.ResolveConfirmConfig([]tui.ConfirmOption{
		tui.WithConfirmHide("exit", "esc"),
	})
	m := newConfirmModel("Sure?", cfg)
	hints := m.Hints()
	if containsString(hints, "ctrl+c exit") || containsString(hints, "esc back") {
		t.Errorf("hidden entries leaked, got %v", hints)
	}
	// ctrl+c still interrupts.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.(confirmModel).interrupted {
		t.Error("ctrl+c key handling should remain after hide")
	}
}

func TestConfirmBindings_AddBinding(t *testing.T) {
	fired := false
	help := KeyBinding[confirmModel]{
		ID:    "help",
		Match: MatchRune('?'),
		Label: func(*confirmModel) string { return "? help" },
		Handle: func(_ *confirmModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			fired = true
			return nil, true
		},
	}
	cfg := tui.ResolveConfirmConfig([]tui.ConfirmOption{WithConfirmAddBindings(help)})
	m := newConfirmModel("Sure?", cfg)
	if !containsString(m.Hints(), "? help") {
		t.Errorf("custom label missing, got %v", m.Hints())
	}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if !fired {
		t.Error("custom handler did not fire")
	}
}
