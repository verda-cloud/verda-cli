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
	"context"
	"errors"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// runResult holds the outcome of a Bubble Tea program run, including
// whether the exit was caused by SIGINT (Ctrl+C).
type runResult struct {
	model       tea.Model
	err         error
	interrupted bool // true if SIGINT was received during Run
}

// runProgram runs a tea.Program and translates Bubble Tea's ErrInterrupted
// (returned when Ctrl+C / SIGINT is received) into our tui.ErrInterrupted.
//
// In Bubble Tea v2, Ctrl+C generates SIGINT which the framework catches and
// returns as tea.ErrInterrupted from program.Run(). The model never sees
// the key event. This method detects that and sets the interrupted flag.
func (p *Prompter) runProgram(ctx context.Context, model tea.Model) runResult {
	// Without terminal stdin (pipe, redirect, /dev/null) a prompt can never
	// receive keys and bubbletea would redraw forever — fail fast instead.
	if f, ok := p.in.(*os.File); ok && !term.IsTerminal(f.Fd()) {
		return runResult{err: tui.ErrNoTerminal}
	}

	program := tea.NewProgram(model,
		tea.WithInput(p.in),
		tea.WithOutput(p.out),
		tea.WithContext(ctx),
	)

	result, err := program.Run()

	// Bubble Tea returns tea.ErrInterrupted when SIGINT (Ctrl+C) is received.
	interrupted := errors.Is(err, tea.ErrInterrupted)

	// Also check the model's interrupted flag (in case raw mode
	// delivered Ctrl+C as a key event rather than a signal).
	if !interrupted {
		switch m := result.(type) {
		case selectModel:
			interrupted = m.interrupted
		case multiSelectModel:
			interrupted = m.interrupted
		case textInputModel:
			interrupted = m.interrupted
		case confirmModel:
			interrupted = m.interrupted
		case passwordModel:
			interrupted = m.interrupted
		}
	}

	// Clear the framework error if we're handling it as interrupted.
	if interrupted {
		err = nil
	}

	return runResult{model: result, err: err, interrupted: interrupted}
}

// Prompter implements tui.Prompter using Bubbletea.
type Prompter struct {
	in      io.Reader
	out     io.Writer // interactive UI: prompts, spinner, progress, pager scroller
	errOut  io.Writer
	dataOut io.Writer // data output: Table, pager print-through
}

// New creates a Bubbletea-backed Prompter.
func New(ioOpts ...func(*Prompter)) *Prompter {
	p := &Prompter{
		in:      os.Stdin,
		out:     os.Stdout,
		errOut:  os.Stderr,
		dataOut: os.Stdout,
	}
	for _, o := range ioOpts {
		o(p)
	}
	return p
}

// WithIO configures the prompter with custom IO streams. The split follows the
// house rule "prompts → ErrOut, data → Out": interactive UI (Select, Confirm,
// TextInput, spinner, progress, the pager scroller) renders on ErrOut, while
// data (Table, pager print-through) is written to Out.
func WithIO(io tui.IO) func(*Prompter) {
	return func(p *Prompter) {
		if io.In != nil {
			p.in = io.In
		}
		if io.Out != nil {
			p.dataOut = io.Out
		}
		if io.ErrOut != nil {
			p.out = io.ErrOut
			p.errOut = io.ErrOut
		}
	}
}

// NewFromIO adapts tui.IO modifiers into a Prompter; used by the registered
// Default/DefaultStatus builders.
func NewFromIO(ioOpts ...func(*tui.IO)) *Prompter {
	var io tui.IO
	for _, o := range ioOpts {
		o(&io)
	}
	return New(WithIO(io))
}

// Compile-time interface checks.
var _ tui.Prompter = (*Prompter)(nil)
var _ tui.Status = (*Prompter)(nil)
var _ tui.LiveLister = (*Prompter)(nil)

func init() {
	tui.RegisterBuilder(func(ioOpts ...func(*tui.IO)) tui.Prompter {
		return NewFromIO(ioOpts...)
	})
	tui.RegisterStatusBuilder(func(ioOpts ...func(*tui.IO)) tui.Status {
		return NewFromIO(ioOpts...)
	})
}
