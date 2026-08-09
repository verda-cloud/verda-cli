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

import tea "charm.land/bubbletea/v2"

// GoBackMsg is returned as a tea.Cmd by prompt models when the user
// presses Esc and there is no internal state to clear (e.g. no active filter).
// The composite model receives this and navigates back.
type GoBackMsg struct{}

// PromptModel is a tea.Model that reports domain-level hints and completion state.
// Used by the wizard composite model to embed prompts without running separate programs.
type PromptModel interface {
	tea.Model

	// Hints returns prompt-specific key hints (e.g. "↑/↓ navigate", "enter select").
	// These are merged with wizard-level hints in the hint bar.
	Hints() []string

	// Result returns the prompt outcome after the user submits.
	// done=false means the prompt is still active.
	Result() (value any, done bool)
}
