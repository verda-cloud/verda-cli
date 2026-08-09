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
	"context"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// PromptType defines which TUI widget to use for a step.
type PromptType int

const (
	SelectPrompt      PromptType = iota // Single selection from a list.
	MultiSelectPrompt                   // Multiple selections from a list.
	TextInputPrompt                     // Free-form text input.
	ConfirmPrompt                       // Yes/No confirmation.
	PasswordPrompt                      // Masked text input.
)

// Choice represents one selectable option in a list prompt.
type Choice struct {
	Label       string // Displayed to the user.
	Value       string // Actual value stored in collected map.
	Description string // Optional extra info.
}

// LoaderFunc fetches available choices for a step.
// It receives the Prompter for sub-prompts, Status for spinners/progress,
// and Store for reading/writing shared data.
// Use store.Collected() to access values from previously completed steps.
type LoaderFunc func(ctx context.Context, prompter tui.Prompter, status tui.Status, store *Store) ([]Choice, error)

// Step defines one step in the wizard flow.
type Step struct {
	Name        string
	Description string
	Prompt      PromptType
	Required    bool
	Default     func(collected map[string]any) any
	ShouldSkip  func(collected map[string]any) bool
	Loader      LoaderFunc
	Validate    func(value any) error
	Setter      func(value any)
	Resetter    func()      // Called when step value is cleared (back/skip). Resets the bound variable.
	IsSet       func() bool // Returns true if value was provided via flag/config.
	Value       func() any  // Returns the current value when IsSet is true. Propagates to collected map.
	DependsOn   []string

	// MinError customizes the "minimum selections" validation message for
	// MultiSelectPrompt steps. The func receives the enforced minimum (1
	// for Required steps). nil uses the library default. Ignored by other
	// prompt types. Useful to replace the grammatically awkward default
	// ("at least 1 selections required") on required multi-selects.
	MinError func(min int) string
}

// Flow defines a complete wizard execution graph.
type Flow struct {
	Name   string
	Steps  []Step
	Layout []ViewDef // optional; nil = default layout (progress bar)
}

// StaticChoices returns a LoaderFunc that always returns the given choices.
// Use for steps with fixed options that don't require an API call.
func StaticChoices(choices ...Choice) LoaderFunc {
	return func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
		return choices, nil
	}
}
