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
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func TestTextInputBindings_DefaultHintsOrder(t *testing.T) {
	cfg := tui.ResolveTextInputConfig(nil)
	m := newTextInputModel("Name?", cfg)
	got := strings.Join(m.Hints(), " · ")
	want := "enter submit · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("default hint order drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestTextInputBindings_EnterSubmits(t *testing.T) {
	m := newTextInputModel("Name?", tui.ResolveTextInputConfig(nil))
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !updated.(textInputModel).submitted {
		t.Error("enter should submit")
	}
}

func TestTextInputBindings_EscAborts(t *testing.T) {
	m := newTextInputModel("Name?", tui.ResolveTextInputConfig(nil))
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	if _, ok := cmd().(GoBackMsg); !ok {
		t.Error("esc should produce GoBackMsg")
	}
}

func TestTextInputBindings_CtrlCInterrupts(t *testing.T) {
	m := newTextInputModel("Name?", tui.ResolveTextInputConfig(nil))
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.(textInputModel).interrupted {
		t.Error("ctrl+c should set interrupted")
	}
}

// Validation runs on Enter; failed validation prevents submission and
// surfaces the error in the next View.
func TestTextInputBindings_ValidationBlocksSubmit(t *testing.T) {
	cfg := tui.ResolveTextInputConfig([]tui.TextInputOption{
		tui.WithDefault("bad"),
		tui.WithValidation(func(s string) error {
			if s == "bad" {
				return errors.New("nope")
			}
			return nil
		}),
	})
	m := newTextInputModel("Name?", cfg)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(textInputModel)
	if got.submitted {
		t.Error("validation failure should block submission")
	}
	if got.err == nil {
		t.Error("expected validation error to be set")
	}
	if cmd != nil {
		// shouldn't quit on validation failure
		if msg := cmd(); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Error("validation failure should not quit")
			}
		}
	}
}

// pristine: first printable keystroke clears a pre-filled default before
// the textinput bubble appends.
func TestTextInputBindings_PristineClearsDefault(t *testing.T) {
	cfg := tui.ResolveTextInputConfig([]tui.TextInputOption{tui.WithDefault("hello")})
	m := newTextInputModel("Name?", cfg)
	if m.textInput.Value() != "hello" {
		t.Fatalf("setup: expected default 'hello', got %q", m.textInput.Value())
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got := updated.(textInputModel)
	if got.pristine {
		t.Error("pristine should be cleared after first keystroke")
	}
	if !strings.HasPrefix(got.textInput.Value(), "x") {
		t.Errorf("first keystroke should replace default; value = %q", got.textInput.Value())
	}
	if got.textInput.Value() == "hellox" {
		t.Error("default was not cleared before append")
	}
}

func TestTextInputBindings_Relabel(t *testing.T) {
	cfg := tui.ResolveTextInputConfig([]tui.TextInputOption{
		tui.WithTextInputRelabel("submit", "↵ save"),
	})
	m := newTextInputModel("Name?", cfg)
	if !containsString(m.Hints(), "↵ save") {
		t.Errorf("expected relabel, got %v", m.Hints())
	}
}

func TestTextInputBindings_Hide(t *testing.T) {
	cfg := tui.ResolveTextInputConfig([]tui.TextInputOption{
		tui.WithTextInputHide("exit"),
	})
	m := newTextInputModel("Name?", cfg)
	if containsString(m.Hints(), "ctrl+c exit") {
		t.Errorf("exit should be hidden, got %v", m.Hints())
	}
	// Key still works.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.(textInputModel).interrupted {
		t.Error("ctrl+c should still interrupt after hide")
	}
}

func TestTextInputBindings_AddBinding(t *testing.T) {
	fired := false
	help := KeyBinding[textInputModel]{
		ID:    "help",
		Match: MatchRune('?', tea.ModCtrl),
		Label: func(*textInputModel) string { return "ctrl+? help" },
		Handle: func(_ *textInputModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			fired = true
			return nil, true
		},
	}
	cfg := tui.ResolveTextInputConfig([]tui.TextInputOption{WithTextInputAddBindings(help)})
	m := newTextInputModel("Name?", cfg)
	if !containsString(m.Hints(), "ctrl+? help") {
		t.Errorf("custom label missing, got %v", m.Hints())
	}
	m.Update(tea.KeyPressMsg{Code: '?', Mod: tea.ModCtrl})
	if !fired {
		t.Error("custom handler did not fire")
	}
}
