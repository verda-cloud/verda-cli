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
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func TestTextInputPrompt_Hints(t *testing.T) {
	cfg := tui.ResolveTextInputConfig(nil)
	m := NewTextInputPrompt("Name:", cfg)
	hints := m.Hints()
	if len(hints) == 0 {
		t.Fatal("expected hints")
	}
}

func TestTextInputPrompt_TypeAndEnter(t *testing.T) {
	cfg := tui.ResolveTextInputConfig(nil)
	m := NewTextInputPrompt("Name:", cfg)

	// Type "hello"
	for _, ch := range "hello" {
		updated, _ := m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(PromptModel)
	}

	// Press Enter
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PromptModel)

	val, done := m.Result()
	if !done {
		t.Fatal("expected done after Enter")
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestTextInputPrompt_EscGoBack(t *testing.T) {
	cfg := tui.ResolveTextInputConfig(nil)
	m := NewTextInputPrompt("Name:", cfg)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from Esc")
	}
	msg := cmd()
	if _, ok := msg.(GoBackMsg); !ok {
		t.Fatalf("expected GoBackMsg, got %T", msg)
	}
}

func TestTextInputPrompt_Result_NotDoneInitially(t *testing.T) {
	cfg := tui.ResolveTextInputConfig(nil)
	m := NewTextInputPrompt("Name:", cfg)
	_, done := m.Result()
	if done {
		t.Fatal("should not be done initially")
	}
}
