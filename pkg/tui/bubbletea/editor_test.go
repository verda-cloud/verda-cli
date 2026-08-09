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

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func newEd(opts ...func(*tui.EditorConfig)) editorModel {
	cfg := tui.EditorConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return newEditorModel("Notes", cfg)
}

func TestEditorModel_DefaultHint(t *testing.T) {
	m := newEd()
	if !strings.Contains(m.View().Content, "(ctrl+d to submit, esc to cancel)") {
		t.Errorf("expected default hint, got %q", m.View().Content)
	}
}

func TestEditorModel_HintOverride(t *testing.T) {
	m := newEd(func(c *tui.EditorConfig) { c.Hint = "⌘↵ to save" })
	if !strings.Contains(m.View().Content, "(⌘↵ to save)") {
		t.Errorf("expected overridden hint, got %q", m.View().Content)
	}
}

func TestEditorModel_NoHint(t *testing.T) {
	m := newEd(func(c *tui.EditorConfig) { c.NoHint = true })
	if strings.Contains(m.View().Content, "(") {
		t.Errorf("expected no affordance parentheses, got %q", m.View().Content)
	}
}

func TestEditorModel_NoHintBeatsHint(t *testing.T) {
	m := newEd(func(c *tui.EditorConfig) {
		c.Hint = "should not show"
		c.NoHint = true
	})
	if strings.Contains(m.View().Content, "should not show") {
		t.Errorf("NoHint should take precedence over Hint, got %q", m.View().Content)
	}
}

func TestEditorModel_DefaultSummary(t *testing.T) {
	m := newEd(func(c *tui.EditorConfig) { c.Default = "a\nb\nc" })
	m.submitted = true
	if !strings.Contains(m.View().Content, "[3 lines]") {
		t.Errorf("expected default summary, got %q", m.View().Content)
	}
}

func TestEditorModel_SummaryOverride(t *testing.T) {
	m := newEd(func(c *tui.EditorConfig) {
		c.Default = "a\nb"
		c.Summary = func(lines int) string { return fmt.Sprintf("saved %d line(s)", lines) }
	})
	m.submitted = true
	if !strings.Contains(m.View().Content, "saved 2 line(s)") {
		t.Errorf("expected overridden summary, got %q", m.View().Content)
	}
}
