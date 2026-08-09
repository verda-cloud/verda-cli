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
	"image/color"
	"reflect"
	"strings"

	"charm.land/lipgloss/v2"
)

// HintBarView renders contextual keyboard hints based on the current
// step's PromptType. It subscribes to StepChangedMsg and updates
// automatically as the wizard progresses.
type HintBarView struct {
	promptType PromptType
	style      lipgloss.Style
	sepStyle   lipgloss.Style
}

// HintBarOption configures the HintBarView.
type HintBarOption func(*HintBarView)

// WithHintColor sets the foreground color for hint text and separators.
func WithHintColor(c color.Color) HintBarOption {
	return func(v *HintBarView) {
		v.style = lipgloss.NewStyle().Foreground(c)
		v.sepStyle = lipgloss.NewStyle().Foreground(c)
	}
}

// WithHintStyle sets the full style for hint text and separators.
// Use this for no-color themes where Faint/Bold is needed instead of colors.
func WithHintStyle(s lipgloss.Style) HintBarOption {
	return func(v *HintBarView) {
		v.style = s
		v.sepStyle = s
	}
}

// NewHintBarView creates a HintBarView.
func NewHintBarView(opts ...HintBarOption) *HintBarView {
	v := &HintBarView{
		promptType: -1, // no step yet
		style:      lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		sepStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Update handles StepChangedMsg to update the hint based on PromptType.
func (v *HintBarView) Update(msg any) (string, []any) {
	if m, ok := msg.(StepChangedMsg); ok {
		v.promptType = m.PromptType
	}
	return v.render(), nil
}

// Subscribe limits this view to StepChangedMsg only.
func (v *HintBarView) Subscribe() []reflect.Type {
	return []reflect.Type{reflect.TypeOf(StepChangedMsg{})}
}

func (v *HintBarView) render() string {
	hints := v.hintsForPrompt(v.promptType)
	if len(hints) == 0 {
		return ""
	}
	sep := v.sepStyle.Render(" · ")
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = v.style.Render(h)
	}
	return "  " + strings.Join(parts, sep)
}

func (v *HintBarView) hintsForPrompt(pt PromptType) []string {
	switch pt {
	case SelectPrompt:
		return []string{"↑/↓ navigate", "type to filter", "enter select", "esc back"}
	case MultiSelectPrompt:
		return []string{"↑/↓ navigate", "space toggle", "enter confirm", "esc back"}
	case TextInputPrompt:
		return []string{"enter submit", "esc cancel"}
	case ConfirmPrompt:
		return []string{"y/n", "enter confirm"}
	case PasswordPrompt:
		return []string{"enter submit", "esc cancel"}
	default:
		return nil
	}
}
