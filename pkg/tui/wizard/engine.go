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
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/bubbletea"
)

// ErrCancelled matches any user abort of the wizard (Ctrl+C, or Esc with no
// step to go back to). Callers use errors.Is to map it to a clean exit while
// propagating real engine/loader failures.
//
// It is never returned bare: Run returns one of the two values below, each
// wrapping both ErrCancelled and the low-level cause the CLI's cancel
// predicates key on. Keeping the cause attached is what lets Esc and Ctrl+C
// stay distinguishable (cmdutil.IsPromptBack vs IsPromptInterrupt) while
// IsPromptCancel matches either.
var ErrCancelled = errors.New("wizard cancelled")

var (
	// errCancelledInterrupt: Ctrl+C — a deliberate hard exit.
	errCancelledInterrupt = fmt.Errorf("%w: %w", ErrCancelled, tui.ErrInterrupted)
	// errCancelledBack: Esc pressed with no earlier step to return to.
	errCancelledBack = fmt.Errorf("%w: %w", ErrCancelled, context.Canceled)
)

// stepState represents the lifecycle state of a step during execution.
type stepState int

const (
	statePending     stepState = iota // not yet visited
	stateFixed                        // IsSet=true, value from flag/config
	stateSkipped                      // ShouldSkip returned true
	stateAutoSkipped                  // loader returned empty choices (optional step)
	stateCompleted                    // user answered or default applied
)

// stepRuntime holds per-step execution state.
type stepRuntime struct {
	state       stepState
	value       any      // the collected value
	choices     []Choice // cached loader results
	loaded      bool     // true if loader has been called (distinguishes nil from not-loaded)
	rewindCount int      // how many times auto-rewind has targeted this step
}

// maxRewindsPerStep is the maximum number of times the engine will auto-rewind
// to the same step before giving up with an error.
const maxRewindsPerStep = 3

// backLabel is appended to Select/MultiSelect choices when back navigation is possible.
const backLabel = "← Back"

// Engine runs a wizard Flow interactively.
type Engine struct {
	prompter       tui.Prompter
	status         tui.Status
	store          *Store
	bus            *MessageBus
	flow           *Flow         // the flow being executed (set during Run)
	steps          []stepRuntime // per-step runtime state, indexed same as flow.Steps
	current        int
	writer         io.Writer
	reader         io.Reader
	keyBindings    []KeyBinding
	resultOverride chan promptResult // test-only: bypasses composite model
	program        *tea.Program      // the running composite program (nil in test mode)
	resultCh       chan promptResult // channel for receiving prompt results

	// validationMsg is set when a step fails Validate; printed above the
	// re-drawn prompt so a rejected answer isn't a silent redraw.
	validationMsg string
}

// EngineOption configures the Engine.
type EngineOption func(*Engine)

// WithOutput sets the writer for engine messages (defaults to os.Stderr).
func WithOutput(w io.Writer) EngineOption {
	return func(e *Engine) { e.writer = w }
}

// WithKeyBindings sets custom wizard-level key bindings.
func WithKeyBindings(bindings ...KeyBinding) EngineOption {
	return func(e *Engine) { e.keyBindings = bindings }
}

// WithInput sets the input reader for the composite tea.Program (defaults to os.Stdin).
func WithInput(r io.Reader) EngineOption {
	return func(e *Engine) { e.reader = r }
}

// TestResult represents a prompt result for testing.
type TestResult struct {
	Value  any
	Action Action
}

// WithTestResults configures the engine to use pre-built results instead of
// running the composite tea.Program. This is for external package tests.
func WithTestResults(results ...TestResult) EngineOption {
	return func(e *Engine) {
		ch := make(chan promptResult, len(results))
		for _, r := range results {
			ch <- promptResult{value: r.Value, action: r.Action}
		}
		close(ch)
		e.resultOverride = ch
	}
}

// Test result constructors for external package tests.

// SelectResult creates a test result for selecting an index.
func SelectResult(idx int) TestResult { return TestResult{Value: idx, Action: ActionNone} }

// TextResult creates a test result for text input.
func TextResult(text string) TestResult { return TestResult{Value: text, Action: ActionNone} }

// ConfirmResult creates a test result for a confirm prompt.
func ConfirmResult(yes bool) TestResult { return TestResult{Value: yes, Action: ActionNone} }

// MultiSelectResult creates a test result for multi-select.
func MultiSelectResult(indices []int) TestResult {
	return TestResult{Value: indices, Action: ActionNone}
}

// BackResult creates a test result for back navigation (Esc).
func BackResult() TestResult { return TestResult{Action: ActionBack} }

// ExitResult creates a test result for exit (Ctrl+C).
func ExitResult() TestResult { return TestResult{Action: ActionExit} }

// NewEngine creates a wizard engine with the given prompter and optional status.
func NewEngine(prompter tui.Prompter, status tui.Status, opts ...EngineOption) *Engine {
	e := &Engine{
		prompter:    prompter,
		status:      status,
		store:       NewStore(),
		keyBindings: DefaultKeyBindings(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Store returns the engine's shared store.
func (e *Engine) Store() *Store {
	return e.store
}

func (e *Engine) out() io.Writer {
	if e.writer != nil {
		return e.writer
	}
	return os.Stderr
}

// Collected returns the values collected during the flow.
func (e *Engine) Collected() map[string]any {
	return e.store.Collected()
}

// Run executes the flow step by step.
func (e *Engine) Run(ctx context.Context, flow *Flow) error {
	e.current = 0
	e.flow = flow
	e.steps = make([]stepRuntime, len(flow.Steps))
	e.store.Reset()

	// Initialize message bus with layout views (or default).
	e.bus = NewMessageBus()
	layout := flow.Layout
	if layout == nil {
		layout = []ViewDef{{ID: "progress", View: NewProgressView()}}
	}
	for _, def := range layout {
		e.bus.Register(def.ID, def.View)
	}

	// Wire store change notifications to the message bus.
	e.store.onChange = func(key string, value any) {
		e.bus.Broadcast(StoreChangedMsg{Key: key, Value: value})
	}

	// In test mode (WithTestResults), bypass the composite program entirely.
	e.resultCh = e.resultOverride
	e.program = nil

	// Turn SIGINT into a context cancellation so Ctrl+C works during
	// Loader execution, when the terminal is in cooked mode and no
	// bubbletea program is handling key events. Without this, Go's
	// default handler kills the process and leaves the terminal in raw
	// mode.
	if e.resultOverride == nil {
		sigCtx, sigCancel := context.WithCancel(ctx)
		defer sigCancel()
		ctx = sigCtx

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		defer signal.Stop(sigCh)
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-sigCh:
				sigCancel()
				// Drain further SIGINTs so repeated Ctrl+C does not
				// fall through to Go's default and kill the process
				// mid-cleanup, leaving the terminal in raw mode.
				for {
					select {
					case <-sigCh:
					case <-done:
						return
					}
				}
			case <-done:
				return
			}
		}()
	}

	// When a custom reader is set (WithInput, typically pipe-based tests),
	// use a persistent program for the entire wizard. Pipe inputs cannot
	// survive program restart because the first program may consume buffered
	// bytes intended for later prompts.
	//
	// When using real stdin (e.reader == nil), create a fresh program per
	// prompt. This prevents Loaders (which may create their own tea.Programs
	// for sub-flows like prompter.Select/Spinner) from racing with the
	// composite program over stdin.
	if e.reader != nil && e.resultOverride == nil {
		return e.runPersistentProgram(ctx)
	}
	return e.runPerPromptProgram(ctx)
}

// runPersistentProgram runs the wizard with a single persistent composite
// program. Used when a custom reader is set (pipe-based tests) where
// restarting programs would lose buffered input.
func (e *Engine) runPersistentProgram(ctx context.Context) error {
	e.resultCh = make(chan promptResult, 1)
	composite := newCompositeModel(e.keyBindings, e.bus, e.resultCh)
	progOpts := []tea.ProgramOption{
		tea.WithoutSignalHandler(),
		tea.WithOutput(e.out()),
		tea.WithInput(e.reader),
	}
	e.program = tea.NewProgram(&composite, progOpts...)
	progDone := make(chan struct{})
	go func() {
		defer close(progDone)
		_, _ = e.program.Run()
	}()
	defer func() {
		e.program.Quit()
		<-progDone
	}()

	return e.stepLoop(ctx)
}

// runPerPromptProgram runs the wizard creating a fresh composite program for
// each prompt. Loaders execute with no program running, so they can safely
// create their own tea.Programs (spinners, sub-flow prompts) without stdin
// conflicts.
func (e *Engine) runPerPromptProgram(ctx context.Context) error {
	return e.stepLoop(ctx)
}

// stepLoop is the main wizard loop shared by both program modes.
func (e *Engine) stepLoop(ctx context.Context) error {
	for e.current < len(e.flow.Steps) {
		step := e.flow.Steps[e.current]
		col := e.store.Collected()

		// ShouldSkip takes priority over IsSet.
		if step.ShouldSkip != nil && step.ShouldSkip(col) {
			e.transition(e.current, stateSkipped, nil)
			e.current++
			continue
		}

		if step.IsSet != nil && step.IsSet() {
			if err := e.handleFixed(step); err != nil {
				return err
			}
			e.current++
			continue
		}

		choices, err := e.loadChoices(ctx, step)
		if err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}

		if step.Loader != nil && len(choices) == 0 {
			handled, err := e.handleEmptyChoices(step)
			if err != nil {
				return err
			}
			if handled {
				continue
			}
		}

		// Build prompt model and send to composite.
		canGoBack := e.hasEditablePriorStep()
		promptModel := e.buildPromptModel(step, choices, canGoBack)
		if promptModel == nil {
			return fmt.Errorf("step %q: unsupported prompt type: %d", step.Name, step.Prompt)
		}

		// In per-prompt mode (no persistent program), start a fresh program.
		perPrompt := e.program == nil && e.resultOverride == nil
		var progDone chan struct{}
		if perPrompt {
			progDone = e.startProgram()
		}

		if e.program != nil {
			e.program.Send(showPromptMsg{
				model: promptModel,
				stepMsg: StepChangedMsg{
					Current:    e.current + 1,
					Total:      len(e.flow.Steps),
					StepName:   step.Name,
					PromptType: step.Prompt,
					Collected:  e.store.Collected(),
				},
			})
		}

		// Wait for result from composite and process it.
		result := <-e.resultCh
		done, err := e.handlePromptResult(result, step, choices, canGoBack)

		if perPrompt {
			e.stopProgram(progDone)
		}

		if err != nil {
			return err
		}
		if done {
			e.current++
		} else if e.validationMsg != "" {
			// Printed after the prompt program stops so the line lands above
			// the re-drawn prompt instead of interleaving with its renderer.
			_, _ = fmt.Fprintf(e.out(), "  ✗ %s\n", e.validationMsg)
			e.validationMsg = ""
		}
	}
	return nil
}

// startProgram creates and starts a new composite tea.Program for one prompt.
// Returns a channel that closes when the program exits.
// In test mode (resultOverride set), this is a no-op.
func (e *Engine) startProgram() chan struct{} {
	if e.resultOverride != nil {
		return nil // test mode — no real program
	}

	e.resultCh = make(chan promptResult, 1)
	composite := newCompositeModel(e.keyBindings, e.bus, e.resultCh)
	progOpts := []tea.ProgramOption{
		tea.WithoutSignalHandler(),
		tea.WithOutput(e.out()),
	}
	if e.reader != nil {
		progOpts = append(progOpts, tea.WithInput(e.reader))
	}
	e.program = tea.NewProgram(&composite, progOpts...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.program.Run()
	}()
	return done
}

// stopProgram quits the composite program and waits for it to fully exit
// so the terminal is restored before the next Loader or prompt.
func (e *Engine) stopProgram(done chan struct{}) {
	if e.program == nil {
		return
	}
	e.program.Quit()
	<-done
	e.program = nil
}

// handlePromptResult processes the result from a prompt.
// Returns (true, nil) when the step is completed and the engine should advance.
// Returns (false, nil) when the engine should re-prompt or rewind (current adjusted internally).
// Returns (false, err) on fatal error.
func (e *Engine) handlePromptResult(result promptResult, step Step, choices []Choice, canGoBack bool) (bool, error) {
	e.validationMsg = ""
	switch result.action {
	case ActionExit:
		_, _ = fmt.Fprintln(e.out())
		return false, errCancelledInterrupt
	case ActionBack:
		if canGoBack {
			e.rewindOne()
		} else {
			_, _ = fmt.Fprintln(e.out())
			return false, errCancelledBack
		}
		return false, nil
	}

	// ActionNone — prompt completed with a value.
	value := result.value

	// Handle "← Back" selection in select/multiselect.
	if idx, ok := value.(int); ok && canGoBack && idx == len(choices) {
		e.rewindOne()
		return false, nil
	}

	// Convert index to Choice value for select prompts.
	if step.Prompt == SelectPrompt {
		if idx, ok := value.(int); ok {
			value = choices[idx].Value
		}
	}
	// Convert indices to values for multiselect.
	if step.Prompt == MultiSelectPrompt {
		if indices, ok := value.([]int); ok {
			values := make([]string, len(indices))
			for i, idx := range indices {
				values[i] = choices[idx].Value
			}
			value = values
		}
	}

	// Apply default for non-required empty values.
	col := e.store.Collected()
	if !step.Required && isEmpty(value) && step.Default != nil {
		value = step.Default(col)
	}

	// Enforce required.
	if step.Required && isEmpty(value) {
		return false, nil // re-prompt
	}

	// Validate. On failure the step re-prompts; the error is printed above the
	// re-drawn prompt instead of being silently dropped. TextInput steps
	// usually never get here — the prompt model validates inline and blocks
	// submission (see buildPromptModel).
	if step.Validate != nil {
		if err := step.Validate(value); err != nil {
			e.validationMsg = err.Error()
			return false, nil // re-prompt
		}
	}

	// Complete the step.
	if step.Setter != nil {
		step.Setter(value)
	}
	e.transition(e.current, stateCompleted, value)

	e.bus.Broadcast(CollectedChangedMsg{
		Key:       step.Name,
		Value:     value,
		Collected: e.store.Collected(),
	})

	e.invalidateDownstream(e.current)
	return true, nil
}

// buildPromptModel creates the appropriate PromptModel for a step.
func (e *Engine) buildPromptModel(step Step, choices []Choice, canGoBack bool) bubbletea.PromptModel {
	switch step.Prompt {
	case SelectPrompt:
		labels := choiceLabels(choices)
		if canGoBack {
			labels = append(labels, backLabel)
		}
		var opts []tui.SelectOption
		if step.Default != nil {
			col := e.store.Collected()
			if defVal, ok := step.Default(col).(string); ok {
				for i, c := range choices {
					if c.Value == defVal {
						opts = append(opts, tui.WithSelectDefault(i))
						break
					}
				}
			}
		}
		cfg := tui.ResolveSelectConfig(opts)
		return bubbletea.NewSelectPrompt(promptLabel(step), labels, cfg)
	case MultiSelectPrompt:
		labels := choiceLabels(choices)
		if canGoBack {
			labels = append(labels, backLabel)
		}
		var opts []tui.MultiSelectOption
		if step.Required {
			opts = append(opts, tui.WithMinSelections(1))
		}
		if step.MinError != nil {
			opts = append(opts, tui.WithMinSelectionsError(step.MinError))
		}
		if step.Default != nil {
			col := e.store.Collected()
			if defVals, ok := step.Default(col).([]string); ok && len(defVals) > 0 {
				valSet := make(map[string]bool, len(defVals))
				for _, v := range defVals {
					valSet[v] = true
				}
				var defaults []int
				for i, c := range choices {
					if valSet[c.Value] {
						defaults = append(defaults, i)
					}
				}
				if len(defaults) > 0 {
					opts = append(opts, tui.WithMultiSelectDefaults(defaults))
				}
			}
		}
		cfg := tui.ResolveMultiSelectConfig(opts)
		return bubbletea.NewMultiSelectPrompt(promptLabel(step), labels, cfg)
	case TextInputPrompt:
		var opts []tui.TextInputOption
		if step.Default != nil {
			col := e.store.Collected()
			if d, ok := step.Default(col).(string); ok && d != "" {
				opts = append(opts, tui.WithDefault(d))
			}
		}
		if step.Validate != nil {
			validate := step.Validate
			opts = append(opts, tui.WithValidation(func(s string) error {
				// The engine substitutes Default for empty input on optional
				// steps; validating "" here would wrongly reject that path.
				if !step.Required && s == "" {
					return nil
				}
				return validate(s)
			}))
		}
		cfg := tui.ResolveTextInputConfig(opts)
		return bubbletea.NewTextInputPrompt(promptLabel(step), cfg)
	case ConfirmPrompt:
		var opts []tui.ConfirmOption
		if step.Default != nil {
			col := e.store.Collected()
			if d, ok := step.Default(col).(bool); ok {
				opts = append(opts, tui.WithConfirmDefault(d))
			}
		}
		cfg := tui.ResolveConfirmConfig(opts)
		return bubbletea.NewConfirmPrompt(promptLabel(step), cfg)
	case PasswordPrompt:
		return bubbletea.NewPasswordPrompt(promptLabel(step))
	default:
		return nil
	}
}

// handleFixed processes a step with IsSet=true.
func (e *Engine) handleFixed(step Step) error {
	if step.Value != nil {
		val := step.Value()
		if step.Validate != nil {
			if err := step.Validate(val); err != nil {
				return fmt.Errorf("step %q: preset value invalid: %w", step.Name, err)
			}
		}
		if step.Setter != nil {
			step.Setter(val)
		}
		e.steps[e.current].state = stateFixed
		e.steps[e.current].value = val
		e.store.SetCollected(step.Name, val)
	} else {
		e.steps[e.current].state = stateFixed
	}
	return nil
}

// handleEmptyChoices handles the case when a step's loader returns no choices.
func (e *Engine) handleEmptyChoices(step Step) (bool, error) {
	col := e.store.Collected()
	if !step.Required {
		if step.Default != nil {
			val := step.Default(col)
			if step.Setter != nil {
				step.Setter(val)
			}
			e.transition(e.current, stateAutoSkipped, val)
		} else {
			e.transition(e.current, stateAutoSkipped, nil)
		}
		e.current++
		return true, nil
	}

	if e.current == 0 {
		return false, fmt.Errorf("step %q: no options available and cannot go back", step.Name)
	}

	e.steps[e.current].rewindCount++
	if e.steps[e.current].rewindCount > maxRewindsPerStep {
		return false, fmt.Errorf("step %q: no options available after %d attempts — the flow cannot proceed with current inputs", step.Name, maxRewindsPerStep)
	}

	_, _ = fmt.Fprintf(e.out(), "  No options available for %q — going back.\n", promptLabel(step))
	if err := e.rewindToDependency(step); err != nil {
		return false, fmt.Errorf("step %q: %w", step.Name, err)
	}
	return true, nil
}

// --- State transitions ---

// transition sets a step to a new state, resetting the caller-bound variable if needed.
func (e *Engine) transition(idx int, newState stepState, value any) {
	rt := &e.steps[idx]
	step := e.flow.Steps[idx]

	// Call Resetter when clearing a step's value (moving to pending, skipped,
	// or auto-skipped without a value). This ensures the caller-bound variable
	// is always consistent with the engine state.
	shouldReset := (newState == statePending || newState == stateSkipped) ||
		(newState == stateAutoSkipped && value == nil)
	if shouldReset && step.Resetter != nil {
		step.Resetter()
	}

	rt.state = newState
	rt.value = value
	rt.choices = nil // invalidate cached choices
	rt.loaded = false
	// Only forward progress clears the auto-rewind guard. Resetting it on
	// every transition was self-defeating: the rewind that follows an empty
	// loader resets the very step that counted the attempt, so the guard
	// never fired and a failing loader bounced the user back forever.
	if newState == stateCompleted {
		rt.rewindCount = 0
	}

	// Keep store in sync.
	if value != nil {
		e.store.SetCollected(step.Name, value)
	} else {
		e.store.ClearCollected(step.Name)
	}
}

// resetRange resets all non-fixed steps in [from, to) to statePending.
func (e *Engine) resetRange(from, to int) {
	for i := from; i < to; i++ {
		if e.steps[i].state == stateFixed {
			continue // preserve preset values
		}
		e.transition(i, statePending, nil)
	}
}

// --- Navigation ---

// rewindOne goes back to the nearest editable prior step, clearing everything between.
func (e *Engine) rewindOne() {
	from := e.current
	e.current--
	for e.current >= 0 {
		if e.isEditable(e.current) {
			e.resetRange(e.current, from+1) // +1 to include the abandoned step
			return
		}
		e.current--
	}
	e.current = 0
}

// rewindToDependency goes to the nearest editable dependency, or the earliest
// editable step if deps are skipped. Returns error if no rewind target exists.
func (e *Engine) rewindToDependency(current Step) error {
	if len(current.DependsOn) == 0 {
		e.rewindOne()
		return nil
	}

	depSet := make(map[string]bool, len(current.DependsOn))
	for _, d := range current.DependsOn {
		depSet[d] = true
	}

	// Classify dependencies.
	nearestEditable := -1
	hasFixed := false
	hasSkipped := false
	for i := e.current - 1; i >= 0; i-- {
		step := e.flow.Steps[i]
		if !depSet[step.Name] {
			continue
		}
		switch {
		case e.steps[i].state == stateFixed:
			hasFixed = true
		case !e.isEditable(i):
			hasSkipped = true
		default:
			if nearestEditable == -1 || i > nearestEditable {
				nearestEditable = i
			}
		}
	}

	// Direct editable dependency found.
	if nearestEditable >= 0 {
		e.resetRange(nearestEditable, e.current)
		e.current = nearestEditable
		return nil
	}

	// All deps are fixed — unrecoverable.
	if hasFixed && !hasSkipped {
		return fmt.Errorf("step %q has no options available and all its dependencies %v are fixed (set via flag)", current.Name, current.DependsOn)
	}

	// Deps are skipped — find the earliest editable step that could change
	// the skip condition.
	if hasSkipped {
		for i := 0; i < e.current; i++ {
			if e.isEditable(i) {
				e.resetRange(i, e.current+1)
				e.current = i
				return nil
			}
		}
		return fmt.Errorf("step %q has no options available and no editable prior step exists to change the outcome", current.Name)
	}

	// Fallback.
	e.rewindOne()
	return nil
}

// isEditable returns true if a step can be prompted (not fixed, not skipped, not auto-skipped).
func (e *Engine) isEditable(idx int) bool {
	step := e.flow.Steps[idx]
	rt := e.steps[idx]
	if rt.state == stateFixed {
		return false
	}
	col := e.store.Collected()
	if step.ShouldSkip != nil && step.ShouldSkip(col) {
		return false
	}
	if rt.state == stateAutoSkipped {
		return false
	}
	return true
}

// hasEditablePriorStep returns true if there is at least one earlier editable step.
func (e *Engine) hasEditablePriorStep() bool {
	for i := e.current - 1; i >= 0; i-- {
		if e.isEditable(i) {
			return true
		}
	}
	return false
}

// --- Choice loading ---

func (e *Engine) loadChoices(ctx context.Context, step Step) ([]Choice, error) {
	if step.Loader == nil {
		return nil, nil
	}

	rt := &e.steps[e.current]
	if rt.loaded {
		return rt.choices, nil
	}

	choices, err := step.Loader(ctx, e.prompter, e.status, e.store)
	if err != nil {
		return nil, err
	}
	rt.choices = choices
	rt.loaded = true
	return choices, nil
}

func (e *Engine) invalidateDownstream(changedIdx int) {
	changedName := e.flow.Steps[changedIdx].Name
	for i, step := range e.flow.Steps {
		if slices.Contains(step.DependsOn, changedName) {
			e.steps[i].choices = nil
			e.steps[i].loaded = false
		}
	}
}

// --- Utilities ---

// promptLabel returns the display text for a step, falling back to Name if Description is empty.
func promptLabel(step Step) string {
	if step.Description != "" {
		return step.Description
	}
	return step.Name
}

func choiceLabels(choices []Choice) []string {
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = c.Label
	}
	return labels
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case []string:
		return len(val) == 0
	default:
		return false
	}
}
