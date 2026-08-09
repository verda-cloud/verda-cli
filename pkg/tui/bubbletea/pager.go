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
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// terminalHeight returns the terminal height for the given writer,
// defaulting to 24 if detection fails (e.g., piped output).
func terminalHeight(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		_, h, err := term.GetSize(f.Fd())
		if err == nil && h > 0 {
			return h
		}
	}
	return 24
}

type pagerModel struct {
	viewport    viewport.Model
	title       string
	ready       bool
	quitting    bool
	interrupted bool // true for Ctrl+C (hard cancel), false for q/Esc
}

func newPagerModel(content string, cfg tui.PagerConfig) pagerModel {
	// Add line numbers if requested.
	if cfg.LineNumbers {
		lines := strings.Split(content, "\n")
		width := len(fmt.Sprintf("%d", len(lines)))
		for i, line := range lines {
			lines[i] = fmt.Sprintf("%*d  %s", width, i+1, line)
		}
		content = strings.Join(lines, "\n")
	}

	m := pagerModel{
		title: cfg.Title,
	}
	// Viewport will be sized on first WindowSizeMsg.
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(24))
	m.viewport.SetContent(content)
	return m
}

func (m pagerModel) Init() tea.Cmd {
	return nil
}

func (m pagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 0
		if m.title != "" {
			headerHeight = 1
		}
		footerHeight := 1
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(msg.Height - headerHeight - footerHeight)
		m.ready = true
	case tea.KeyPressMsg:
		switch msg.String() {
		case keyCtrlC:
			m.quitting = true
			m.interrupted = true
			return m, tea.Quit
		case "q", keyEsc:
			m.quitting = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m pagerModel) View() tea.View {
	if !m.ready {
		return tea.NewView("Loading...")
	}

	var b strings.Builder

	if m.title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n")
	}

	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer with scroll position.
	pct := m.viewport.ScrollPercent() * 100
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	footer := fmt.Sprintf("  ↑/↓ scroll • q quit • %.0f%%", pct)
	b.WriteString(footerStyle.Render(footer))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// Pager implements tui.Status.
func (p *Prompter) Pager(ctx context.Context, content string, opts ...tui.PagerOption) error {
	cfg := tui.ResolvePagerConfig(opts)

	// Auto-detect: if content fits in terminal, just print it.
	lines := strings.Count(content, "\n") + 1
	termHeight := terminalHeight(p.out)
	if lines <= termHeight-2 { // leave room for prompt
		_, err := fmt.Fprint(p.out, content)
		return err
	}

	model := newPagerModel(content, cfg)

	program := tea.NewProgram(model,
		tea.WithInput(p.in),
		tea.WithOutput(p.out),
		tea.WithContext(ctx),
	)

	result, err := program.Run()
	if err != nil {
		return err
	}
	if m, ok := result.(pagerModel); ok && m.interrupted {
		return tui.ErrInterrupted
	}
	return nil
}
