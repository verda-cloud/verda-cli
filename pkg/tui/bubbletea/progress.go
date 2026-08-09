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
	"sync"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// --- Messages ---

type progressSetMsg struct{ percent float64 }
type progressIncrMsg struct{ delta float64 }
type progressStopMsg struct{ finalMessage string }

// --- Model ---

type progressModel struct {
	progress     progress.Model
	message      string
	percent      float64
	done         bool
	finalMessage string
	autoStop     bool
}

func newProgressModel(message string, cfg tui.ProgressConfig) progressModel {
	var opts []progress.Option
	opts = append(opts, progress.WithWidth(cfg.Width))
	if !cfg.ShowPercent {
		opts = append(opts, progress.WithoutPercentage())
	}
	if cfg.SolidFill != "" {
		opts = append(opts, progress.WithColors(lipgloss.Color(cfg.SolidFill)))
	} else {
		opts = append(opts, progress.WithColors(lipgloss.Color(cfg.ColorA), lipgloss.Color(cfg.ColorB)))
	}

	return progressModel{
		progress: progress.New(opts...),
		message:  message,
		autoStop: cfg.AutoStop,
	}
}

func (m progressModel) Init() tea.Cmd { return nil }

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressSetMsg:
		m.percent = msg.percent
		if m.percent > 1.0 {
			m.percent = 1.0
		}
		cmd := m.progress.SetPercent(m.percent)
		if m.autoStop && m.percent >= 1.0 {
			m.done = true
			m.finalMessage = m.message
			return m, tea.Sequence(cmd, tea.Quit)
		}
		return m, cmd
	case progressIncrMsg:
		m.percent += msg.delta
		if m.percent > 1.0 {
			m.percent = 1.0
		}
		cmd := m.progress.SetPercent(m.percent)
		if m.autoStop && m.percent >= 1.0 {
			m.done = true
			m.finalMessage = m.message
			return m, tea.Sequence(cmd, tea.Quit)
		}
		return m, cmd
	case progressStopMsg:
		m.done = true
		if msg.finalMessage != "" {
			m.finalMessage = msg.finalMessage
		} else {
			m.finalMessage = m.message
		}
		return m, tea.Quit
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		if msg.String() == keyCtrlC {
			m.done = true
			m.finalMessage = m.message
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m progressModel) View() tea.View {
	if m.done {
		return tea.NewView(fmt.Sprintf("✓ %s\n", m.finalMessage))
	}
	return tea.NewView(fmt.Sprintf("%s  %s", m.message, m.progress.View()))
}

// --- Handle ---

type progressHandle struct {
	program *tea.Program
	once    sync.Once
	done    chan struct{}
}

func (h *progressHandle) SetPercent(p float64) {
	h.program.Send(progressSetMsg{percent: p})
}

func (h *progressHandle) Increment(delta float64) {
	h.program.Send(progressIncrMsg{delta: delta})
}

func (h *progressHandle) Stop(finalMessage string) {
	h.once.Do(func() {
		h.program.Send(progressStopMsg{finalMessage: finalMessage})
		<-h.done
	})
}

// Progress implements tui.Status.
func (p *Prompter) Progress(ctx context.Context, message string, opts ...tui.ProgressOption) (tui.ProgressHandle, error) {
	cfg := tui.ResolveProgressConfig(opts)
	model := newProgressModel(message, cfg)

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

	return &progressHandle{
		program: program,
		done:    done,
	}, nil
}
