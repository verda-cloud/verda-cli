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
	"fmt"
	"os"
	"sync"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// --- Messages ---

type spinnerUpdateMsg struct{ message string }
type spinnerStopMsg struct{ finalMessage string }

// --- Model ---

type spinnerModel struct {
	spinner      spinner.Model
	message      string
	done         bool
	finalMessage string
	doneSymbol   string
}

func newSpinnerModel(message string, cfg tui.SpinnerConfig) spinnerModel {
	s := spinner.New(spinner.WithSpinner(mapSpinnerStyle(cfg.Style)))
	return spinnerModel{
		spinner:    s,
		message:    message,
		doneSymbol: cfg.DoneSymbol,
	}
}

func (m spinnerModel) Init() tea.Cmd { return m.spinner.Tick }

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerUpdateMsg:
		m.message = msg.message
		return m, nil
	case spinnerStopMsg:
		m.done = true
		if msg.finalMessage != "" {
			m.finalMessage = msg.finalMessage
		} else {
			m.finalMessage = m.message
		}
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		if msg.String() == keyCtrlC {
			m.done = true
			m.finalMessage = m.message
			// Re-raise SIGINT so whoever installed a signal handler
			// (typically the wizard engine) can cancel the outer context
			// and abort the underlying work — tea.Quit alone only tears
			// down the spinner, leaving fn() running.
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				_ = p.Signal(os.Interrupt)
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m spinnerModel) View() tea.View {
	if m.done {
		return tea.NewView(fmt.Sprintf("%s %s\n", m.doneSymbol, m.finalMessage))
	}
	return tea.NewView(fmt.Sprintf("%s %s", m.spinner.View(), m.message))
}

func mapSpinnerStyle(s tui.SpinnerStyle) spinner.Spinner {
	switch s {
	case tui.SpinnerLine:
		return spinner.Line
	case tui.SpinnerMiniDot:
		return spinner.MiniDot
	case tui.SpinnerJump:
		return spinner.Jump
	case tui.SpinnerPulse:
		return spinner.Pulse
	case tui.SpinnerPoints:
		return spinner.Points
	case tui.SpinnerGlobe:
		return spinner.Globe
	case tui.SpinnerMoon:
		return spinner.Moon
	case tui.SpinnerMeter:
		return spinner.Meter
	case tui.SpinnerEllipsis:
		return spinner.Ellipsis
	default:
		return spinner.Dot
	}
}

// --- Handle ---

type spinnerHandle struct {
	program *tea.Program
	once    sync.Once
	done    chan struct{}
}

func (h *spinnerHandle) UpdateMessage(msg string) {
	h.program.Send(spinnerUpdateMsg{message: msg})
}

func (h *spinnerHandle) Stop(finalMessage string) {
	h.once.Do(func() {
		h.program.Send(spinnerStopMsg{finalMessage: finalMessage})
		<-h.done
	})
}

// Spinner implements tui.Status.
func (p *Prompter) Spinner(ctx context.Context, message string, opts ...tui.SpinnerOption) (tui.SpinnerHandle, error) {
	cfg := tui.ResolveSpinnerConfig(opts)
	model := newSpinnerModel(message, cfg)

	program := tea.NewProgram(model,
		tea.WithInput(p.in),
		tea.WithOutput(p.out),
		tea.WithContext(ctx),
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = program.Run()
	}()

	return &spinnerHandle{
		program: program,
		done:    done,
	}, nil
}
