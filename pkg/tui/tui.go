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

package tui

import (
	"context"
	"io"
)

// Prompter is the core interface for interactive terminal prompts.
// Implementations must be safe for sequential use but need not be concurrent-safe.
type Prompter interface {
	// Confirm asks a yes/no question and returns the boolean answer.
	Confirm(ctx context.Context, prompt string, opts ...ConfirmOption) (bool, error)

	// TextInput asks the user for a single line of text.
	TextInput(ctx context.Context, prompt string, opts ...TextInputOption) (string, error)

	// Password asks the user for sensitive input (masked).
	Password(ctx context.Context, prompt string) (string, error)

	// Select presents a list of choices and returns the selected index.
	Select(ctx context.Context, prompt string, choices []string, opts ...SelectOption) (int, error)

	// MultiSelect presents a list of choices and returns selected indices.
	MultiSelect(ctx context.Context, prompt string, choices []string, opts ...MultiSelectOption) ([]int, error)

	// Editor opens a multi-line text editor and returns the result.
	Editor(ctx context.Context, prompt string, opts ...EditorOption) (string, error)
}

// LiveLister is the optional capability for prompters that support a
// live-updating picker. Kept out of Prompter to avoid breaking fakes
// and alternate engines. Callers type-assert:
//
//	if ll, ok := prompter.(tui.LiveLister); ok {
//	    idx, err := ll.LiveList(ctx, prompt, rows, updates, opts...)
//	}
//
// Contract:
//   - Updates with unknown Key are dropped.
//   - Multiple updates for the same Key: last-write-wins.
//   - Cursor identity is preserved across updates (by Key, not index).
//   - Type-to-filter matches the current Label, so rows may drop in
//     or out of the filtered view as labels change.
//   - Caller owns concurrency, retry, and error handling; renderer
//     styles rows with Err != nil distinctly.
//   - Closing the channel is optional; ctx is the cancellation primitive.
type LiveLister interface {
	LiveList(ctx context.Context, prompt string, rows []LiveRow, updates <-chan LiveListUpdate, opts ...LiveListOption) (int, error)
}

// IO holds the input/output streams for the prompter.
// This mirrors the pattern used in pkg/app for testability.
type IO struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// DefaultIO returns IO wired to os.Stdin/Stdout/Stderr.
func DefaultIO() IO {
	return IO{
		In:     nil, // nil means os.Stdin at runtime — set by implementation
		Out:    nil,
		ErrOut: nil,
	}
}

// --- Confirm Options ---

// ConfirmOption configures a Confirm prompt.
type ConfirmOption func(*ConfirmConfig)

// ConfirmConfig holds resolved Confirm settings.
type ConfirmConfig struct {
	Default       bool              // default answer when user presses Enter
	RelabelByID   map[string]string // override individual binding labels by ID
	HiddenByID    []string          // suppress these binding labels from the hint bar
	ExtraBindings any               // engine-specific extra/replacement bindings
}

// WithConfirmDefault sets the default value for a confirm prompt.
func WithConfirmDefault(v bool) ConfirmOption {
	return func(c *ConfirmConfig) { c.Default = v }
}

// WithConfirmRelabel renames a binding's hint by its ID (yes-no,
// confirm, esc, exit). Unknown IDs are silently ignored.
func WithConfirmRelabel(id, label string) ConfirmOption {
	return func(c *ConfirmConfig) {
		if c.RelabelByID == nil {
			c.RelabelByID = make(map[string]string)
		}
		c.RelabelByID[id] = label
	}
}

// WithConfirmHide suppresses one or more bindings from the hint bar by ID.
func WithConfirmHide(ids ...string) ConfirmOption {
	return func(c *ConfirmConfig) {
		c.HiddenByID = append(c.HiddenByID, ids...)
	}
}

// --- TextInput Options ---

// TextInputOption configures a TextInput prompt.
type TextInputOption func(*TextInputConfig)

// TextInputConfig holds resolved TextInput settings.
type TextInputConfig struct {
	Default       string
	Placeholder   string
	Validate      func(string) error
	RelabelByID   map[string]string // override individual binding labels by ID
	HiddenByID    []string          // suppress these binding labels from the hint bar
	ExtraBindings any               // engine-specific extra/replacement bindings
}

// WithDefault sets the default value for a text input.
func WithDefault(v string) TextInputOption {
	return func(c *TextInputConfig) { c.Default = v }
}

// WithPlaceholder sets the placeholder text.
func WithPlaceholder(v string) TextInputOption {
	return func(c *TextInputConfig) { c.Placeholder = v }
}

// WithValidation sets a validation function for the input.
func WithValidation(fn func(string) error) TextInputOption {
	return func(c *TextInputConfig) { c.Validate = fn }
}

// WithTextInputRelabel renames a binding's hint by its ID (submit,
// esc, exit). Unknown IDs are silently ignored.
func WithTextInputRelabel(id, label string) TextInputOption {
	return func(c *TextInputConfig) {
		if c.RelabelByID == nil {
			c.RelabelByID = make(map[string]string)
		}
		c.RelabelByID[id] = label
	}
}

// WithTextInputHide suppresses one or more bindings from the hint bar by ID.
func WithTextInputHide(ids ...string) TextInputOption {
	return func(c *TextInputConfig) {
		c.HiddenByID = append(c.HiddenByID, ids...)
	}
}

// --- Select Options ---

// SelectOption configures a Select prompt.
type SelectOption func(*SelectConfig)

// SelectConfig holds resolved Select settings.
type SelectConfig struct {
	Default       int               // index of the default selection
	PageSize      int               // number of visible items (0 = show all)
	Loop          bool              // wrap around at ends
	ShowHints     bool              // render the prompt's Hints() bar below the choices
	Hints         []string          // override hint strings entirely; nil = use prompt defaults
	RelabelByID   map[string]string // override individual binding labels by ID
	HiddenByID    []string          // suppress these binding labels from the hint bar
	ExtraBindings any               // engine-specific extra/replacement bindings (set via bubbletea options)
}

// WithSelectDefault sets the default selected index.
func WithSelectDefault(index int) SelectOption {
	return func(c *SelectConfig) { c.Default = index }
}

// WithPageSize sets the number of visible items.
func WithPageSize(n int) SelectOption {
	return func(c *SelectConfig) { c.PageSize = n }
}

// WithLoop enables wrapping when navigating past ends.
func WithLoop(v bool) SelectOption {
	return func(c *SelectConfig) { c.Loop = v }
}

// WithShowHints renders the prompt's Hints() bar below the choices.
// Off by default — wizard flows render hints externally, so callers
// inside a wizard step must NOT set this.
func WithShowHints(v bool) SelectOption {
	return func(c *SelectConfig) { c.ShowHints = v }
}

// WithHints replaces all hint strings (for localization or shorter
// labels). For per-binding rename use WithSelectRelabel; for
// suppression use WithSelectHide.
func WithHints(hints ...string) SelectOption {
	return func(c *SelectConfig) { c.Hints = hints }
}

// WithSelectRelabel renames a binding's hint by its ID (navigate,
// select, esc, exit, …). Unknown IDs are silently ignored.
func WithSelectRelabel(id, label string) SelectOption {
	return func(c *SelectConfig) {
		if c.RelabelByID == nil {
			c.RelabelByID = make(map[string]string)
		}
		c.RelabelByID[id] = label
	}
}

// WithSelectHide suppresses the listed binding labels from the hint
// bar. The keys still trigger their handlers.
func WithSelectHide(ids ...string) SelectOption {
	return func(c *SelectConfig) {
		c.HiddenByID = append(c.HiddenByID, ids...)
	}
}

// --- LiveList Options ---

// LiveRow is one row of a LiveList. Key is the stable identity used
// across updates; Label is the initial display text and what
// type-to-filter matches until replaced.
type LiveRow struct {
	Key   string
	Label string
}

// LiveListUpdate replaces the Label of the row identified by Key.
// Err is a signal flag — even when set, supply a meaningful Label
// ("error: rate limited") since the renderer still shows it.
type LiveListUpdate struct {
	Key   string
	Label string
	Err   error
}

// LiveListOption configures a LiveList prompt.
type LiveListOption func(*LiveListConfig)

// LiveListConfig mirrors SelectConfig. Separate type so live-only
// fields (debounce, etc.) won't widen SelectConfig later.
type LiveListConfig struct {
	Default       int
	PageSize      int
	Loop          bool
	ShowHints     bool
	Hints         []string
	RelabelByID   map[string]string
	HiddenByID    []string
	ExtraBindings any // engine-specific extras (set via bubbletea options)
}

// WithLiveListDefault sets the initial cursor position.
func WithLiveListDefault(index int) LiveListOption {
	return func(c *LiveListConfig) { c.Default = index }
}

// WithLiveListPageSize sets the number of visible rows.
func WithLiveListPageSize(n int) LiveListOption {
	return func(c *LiveListConfig) { c.PageSize = n }
}

// WithLiveListLoop enables cursor wrap.
func WithLiveListLoop(v bool) LiveListOption {
	return func(c *LiveListConfig) { c.Loop = v }
}

// WithLiveListShowHints renders the prompt's Hints() bar below the
// rows. Off by default — must not be set inside a wizard step.
func WithLiveListShowHints(v bool) LiveListOption {
	return func(c *LiveListConfig) { c.ShowHints = v }
}

// WithLiveListHints replaces all hint strings. For per-binding rename
// use WithLiveListRelabel.
func WithLiveListHints(hints ...string) LiveListOption {
	return func(c *LiveListConfig) { c.Hints = hints }
}

// WithLiveListRelabel renames a binding's hint by its ID; same ID
// space as Select (navigate, select, esc, exit, …).
func WithLiveListRelabel(id, label string) LiveListOption {
	return func(c *LiveListConfig) {
		if c.RelabelByID == nil {
			c.RelabelByID = make(map[string]string)
		}
		c.RelabelByID[id] = label
	}
}

// WithLiveListHide suppresses the listed binding labels from the hint
// bar; keys still trigger their handlers.
func WithLiveListHide(ids ...string) LiveListOption {
	return func(c *LiveListConfig) {
		c.HiddenByID = append(c.HiddenByID, ids...)
	}
}

// --- MultiSelect Options ---

// MultiSelectOption configures a MultiSelect prompt.
type MultiSelectOption func(*MultiSelectConfig)

// MultiSelectConfig holds resolved MultiSelect settings.
type MultiSelectConfig struct {
	Defaults      []int // indices selected by default
	PageSize      int
	Loop          bool
	Min           int               // minimum required selections (0 = no minimum)
	Max           int               // maximum allowed selections (0 = no maximum)
	ShowHints     bool              // render the prompt's Hints() bar below the choices
	Hints         []string          // override hint strings entirely; nil = use prompt defaults
	RelabelByID   map[string]string // override individual binding labels by ID
	HiddenByID    []string          // suppress these binding labels from the hint bar
	ExtraBindings any               // engine-specific extra/replacement bindings (set via bubbletea options)

	// MinError / MaxError override the validation messages shown when the
	// selection count is below Min or above Max. nil = library default.
	MinError func(min int) string
	MaxError func(max int) string
}

// WithMultiSelectDefaults sets the default selected indices.
func WithMultiSelectDefaults(indices []int) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.Defaults = indices }
}

// WithMultiSelectPageSize sets the number of visible items.
func WithMultiSelectPageSize(n int) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.PageSize = n }
}

// WithMinSelections sets the minimum required selections.
func WithMinSelections(n int) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.Min = n }
}

// WithMaxSelections sets the maximum allowed selections.
func WithMaxSelections(n int) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.Max = n }
}

// WithMinSelectionsError overrides the message shown when the user
// confirms with fewer than Min selections. The func receives the
// configured minimum. nil (the default) uses the library message.
func WithMinSelectionsError(fn func(min int) string) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.MinError = fn }
}

// WithMaxSelectionsError overrides the message shown when the user
// tries to select more than Max items. The func receives the
// configured maximum. nil (the default) uses the library message.
func WithMaxSelectionsError(fn func(max int) string) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.MaxError = fn }
}

// WithMultiSelectShowHints renders the prompt's Hints() bar below the
// choices. Off by default — must not be set inside a wizard step.
func WithMultiSelectShowHints(v bool) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.ShowHints = v }
}

// WithMultiSelectHints replaces all hint strings. For per-binding
// rename use WithMultiSelectRelabel.
func WithMultiSelectHints(hints ...string) MultiSelectOption {
	return func(c *MultiSelectConfig) { c.Hints = hints }
}

// WithMultiSelectRelabel renames a binding's hint by its ID (navigate,
// toggle, select-all, confirm, esc, exit). Unknown IDs are ignored.
func WithMultiSelectRelabel(id, label string) MultiSelectOption {
	return func(c *MultiSelectConfig) {
		if c.RelabelByID == nil {
			c.RelabelByID = make(map[string]string)
		}
		c.RelabelByID[id] = label
	}
}

// WithMultiSelectHide suppresses the listed binding labels from the
// hint bar; keys still trigger their handlers.
func WithMultiSelectHide(ids ...string) MultiSelectOption {
	return func(c *MultiSelectConfig) {
		c.HiddenByID = append(c.HiddenByID, ids...)
	}
}

// --- Editor Options ---

// EditorOption configures an Editor prompt.
type EditorOption func(*EditorConfig)

// EditorConfig holds resolved Editor settings.
type EditorConfig struct {
	Default  string
	FileExt  string // file extension hint for syntax highlighting
	ShowHelp bool

	// Hint overrides the affordance text shown in parentheses while
	// editing (default: "ctrl+d to submit, esc to cancel"). "" = default.
	Hint string
	// NoHint suppresses the affordance line entirely (no parentheses).
	// Takes precedence over Hint.
	NoHint bool
	// Summary overrides the post-submit summary (default: "[N lines]").
	// The func receives the submitted line count. nil = library default.
	Summary func(lines int) string
}

// WithEditorDefault sets the initial content in the editor.
func WithEditorDefault(v string) EditorOption {
	return func(c *EditorConfig) { c.Default = v }
}

// WithFileExt sets the file extension hint.
func WithFileExt(ext string) EditorOption {
	return func(c *EditorConfig) { c.FileExt = ext }
}

// WithEditorHint overrides the affordance text rendered in parentheses
// while editing. Passing "" keeps the library default; use
// WithEditorNoHint to suppress the affordance entirely.
func WithEditorHint(text string) EditorOption {
	return func(c *EditorConfig) { c.Hint = text }
}

// WithEditorNoHint suppresses the affordance line entirely (no
// parentheses are rendered). Takes precedence over WithEditorHint.
func WithEditorNoHint() EditorOption {
	return func(c *EditorConfig) { c.NoHint = true }
}

// WithEditorSummary overrides the summary shown after the user submits.
// The func receives the line count. nil keeps the library default.
func WithEditorSummary(fn func(lines int) string) EditorOption {
	return func(c *EditorConfig) { c.Summary = fn }
}
