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

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// Table implements tui.Status.
func (p *Prompter) Table(_ context.Context, columns []string, rows [][]string, opts ...tui.TableOption) error {
	cfg := tui.ResolveTableConfig(opts)

	cols := make([]table.Column, len(columns))
	for i, c := range columns {
		width := len(c)
		for _, row := range rows {
			if i < len(row) && len(row[i]) > width {
				width = len(row[i])
			}
		}
		if cfg.MaxWidth > 0 && width > cfg.MaxWidth/len(columns) {
			width = cfg.MaxWidth / len(columns)
		}
		cols[i] = table.Column{Title: c, Width: width}
	}

	tableRows := make([]table.Row, len(rows))
	for i, row := range rows {
		tableRows[i] = row
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(tableRows),
		table.WithHeight(len(rows)+1),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true)
	s.Selected = lipgloss.NewStyle()
	t.SetStyles(s)

	_, err := fmt.Fprintln(p.out, t.View())
	return err
}
